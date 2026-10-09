//go:build windows

package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	procThreadAttributeHandleList           = 0x00020002
	procThreadAttributeSecurityCapabilities = 0x00020009
	extendedStartupInfoPresent              = 0x00080000
	createSuspended                         = 0x00000004
	createUnicodeEnvironment                = 0x00000400
	startfUseStdHandles                     = 0x00000100
	tokenQuery                              = 0x0008
	tokenIsAppContainer                     = 29
	tokenAppContainerSid                    = 31
)
const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitActiveProcess       = 0x00000008
	jobObjectLimitJobTime             = 0x00000004
	jobObjectLimitProcessMemory       = 0x00000100
	jobObjectLimitJobMemory           = 0x00000200
	jobObjectLimitKillOnJobClose      = 0x00002000
	waitObject0                       = 0
	waitTimeout                       = 258
	infinite                          = 0xFFFFFFFF
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	userenv  = syscall.NewLazyDLL("userenv.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateJobObjectW                  = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject           = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject          = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject                = kernel32.NewProc("TerminateJobObject")
	procResumeThread                      = kernel32.NewProc("ResumeThread")
	procWaitForSingleObject               = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess                = kernel32.NewProc("GetExitCodeProcess")
	procLocalFree                         = kernel32.NewProc("LocalFree")
	procGetTokenInformation               = advapi32.NewProc("GetTokenInformation")
	procOpenProcessToken                  = advapi32.NewProc("OpenProcessToken")
	procConvertSidToStringSidW            = advapi32.NewProc("ConvertSidToStringSidW")
	procFreeSid                           = advapi32.NewProc("FreeSid")
	procCreateAppContainerProfile         = userenv.NewProc("CreateAppContainerProfile")
	procDeriveAppContainerSid             = userenv.NewProc("DeriveAppContainerSidFromAppContainerName")
	procDeleteAppContainerProfile         = userenv.NewProc("DeleteAppContainerProfile")
)

type securityCapabilities struct {
	AppContainerSid uintptr
	Capabilities    uintptr
	CapabilityCount uint32
	Reserved        uint32
}

type startupInfoEx struct {
	syscall.StartupInfo
	AttributeList uintptr
}
type jobBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobExtendedLimitInformation struct {
	BasicLimitInformation jobBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}
type agentRunRequest struct {
	OwnerID       string
	ProjectID     string
	Executable    string
	Args          []string
	Dir           string
	Env           map[string]string
	ReadOnlyPaths []string
}

type agentRunResult struct {
	ExitCode        uint32
	Stdout          string
	Stderr          string
	AppContainerSID string
	IsAppContainer  bool
}

type windowsAgentRunner struct {
	cfg AgentConfig
}

func newAgentRunner(cfg AgentConfig) *windowsAgentRunner {
	return &windowsAgentRunner{cfg: cfg}
}

func appContainerName(ownerID string) string {
	return "OnTechChat.Agent." + digest(ownerID)[:24]
}
func appContainerProfile(ownerID string) (uintptr, string, error) {
	name := appContainerName(ownerID)
	namePtr, _ := syscall.UTF16PtrFromString(name)
	displayPtr, _ := syscall.UTF16PtrFromString("OnTechChat Agent")
	descPtr, _ := syscall.UTF16PtrFromString("OnTechChat user agent sandbox")
	var sid uintptr
	r1, _, _ := procCreateAppContainerProfile.Call(
		uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(displayPtr)),
		uintptr(unsafe.Pointer(descPtr)), 0, 0, uintptr(unsafe.Pointer(&sid)),
	)
	hr := uint32(r1)
	if hr != 0 {
		if hr != 0x800700B7 {
			return 0, "", fmt.Errorf("CreateAppContainerProfile HRESULT 0x%08x", hr)
		}
		r1, _, _ = procDeriveAppContainerSid.Call(
			uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(&sid)),
		)
		if uint32(r1) != 0 {
			return 0, "", fmt.Errorf("DeriveAppContainerSid HRESULT 0x%08x", uint32(r1))
		}
	}
	if sid == 0 {
		return 0, "", errors.New("AppContainer SID missing")
	}
	text, err := sidString(sid)
	if err != nil {
		procFreeSid.Call(sid)
		return 0, "", err
	}
	return sid, text, nil
}

func deleteAppContainerProfile(ownerID string) error {
	namePtr, _ := syscall.UTF16PtrFromString(appContainerName(ownerID))
	r1, _, _ := procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(namePtr)))
	if uint32(r1) != 0 {
		return fmt.Errorf("DeleteAppContainerProfile HRESULT 0x%08x", uint32(r1))
	}
	return nil
}

func sidString(sid uintptr) (string, error) {
	var textPtr uintptr
	r1, _, e1 := procConvertSidToStringSidW.Call(sid, uintptr(unsafe.Pointer(&textPtr)))
	if r1 == 0 {
		return "", e1
	}
	defer procLocalFree.Call(textPtr)
	var u16 []uint16
	for p := textPtr; ; p += 2 {
		v := *(*uint16)(unsafe.Pointer(p))
		if v == 0 {
			break
		}
		u16 = append(u16, v)
		if len(u16) > 512 {
			return "", errors.New("SID string too long")
		}
	}
	return syscall.UTF16ToString(u16), nil
}

func appContainerACL(path, sid, rights string) error {
	if path == "" || sid == "" {
		return errors.New("ACL target or SID missing")
	}
	args := []string{path, "/grant", "*" + sid + ":(OI)(CI)" + rights, "/T", "/C", "/Q"}
	out, err := exec.Command("icacls.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls grant failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeAppContainerACL(path, sid string) {
	if path == "" || sid == "" {
		return
	}
	_, _ = exec.Command("icacls.exe", path, "/remove:g", "*"+sid, "/T", "/C", "/Q").CombinedOutput()
}
func createAttributeList(count uint32) ([]byte, uintptr, error) {
	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, uintptr(count), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, 0, errors.New("attribute list size missing")
	}
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	r1, _, e1 := procInitializeProcThreadAttributeList.Call(
		ptr, uintptr(count), 0, uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return nil, 0, e1
	}
	return buf, ptr, nil
}

func updateAttribute(list, attribute, value, size uintptr) error {
	r1, _, e1 := procUpdateProcThreadAttribute.Call(list, 0, attribute, value, size, 0, 0)
	if r1 == 0 {
		return e1
	}
	return nil
}

func buildWindowsEnvironment(values map[string]string, currentDir string) (*uint16, []uint16, error) {
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(values[k], 0) {
			return nil, nil, errors.New("invalid environment")
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToUpper(keys[i]) < strings.ToUpper(keys[j]) })
	var block []uint16
	if volume := filepath.VolumeName(currentDir); len(volume) == 2 && volume[1] == ':' {
		part, err := syscall.UTF16FromString("=" + strings.ToUpper(volume) + "=" + currentDir)
		if err != nil {
			return nil, nil, err
		}
		block = append(block, part...)
	}
	for _, k := range keys {
		part, err := syscall.UTF16FromString(k + "=" + values[k])
		if err != nil {
			return nil, nil, err
		}
		block = append(block, part...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return &block[0], block, nil
}

func commandLine(executable string, args []string) (*uint16, []uint16, error) {
	all := make([]string, 0, len(args)+1)
	all = append(all, executable)
	all = append(all, args...)
	for i := range all {
		all[i] = syscall.EscapeArg(all[i])
	}
	raw := strings.Join(all, " ")
	u16, err := syscall.UTF16FromString(raw)
	if err != nil {
		return nil, nil, err
	}
	return &u16[0], u16, nil
}
func createInheritableFile(path string, access uint32, creation uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), InheritHandle: 1}
	h, err := syscall.CreateFile(
		p, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		&sa, creation, syscall.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		return 0, err
	}
	return h, nil
}

func createAgentIO(dir string) (syscall.Handle, syscall.Handle, syscall.Handle, string, string, func(), error) {
	ioDir, err := os.MkdirTemp(dir, ".agent-io-")
	if err != nil {
		return 0, 0, 0, "", "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(ioDir) }
	stdin, err := createInheritableFile("NUL", syscall.GENERIC_READ, syscall.OPEN_EXISTING)
	if err != nil {
		cleanup()
		return 0, 0, 0, "", "", nil, err
	}
	stdoutPath := filepath.Join(ioDir, "stdout.txt")
	stderrPath := filepath.Join(ioDir, "stderr.txt")
	stdout, err := createInheritableFile(stdoutPath, syscall.GENERIC_WRITE, syscall.CREATE_ALWAYS)
	if err != nil {
		syscall.CloseHandle(stdin)
		cleanup()
		return 0, 0, 0, "", "", nil, err
	}
	stderr, err := createInheritableFile(stderrPath, syscall.GENERIC_WRITE, syscall.CREATE_ALWAYS)
	if err != nil {
		syscall.CloseHandle(stdin)
		syscall.CloseHandle(stdout)
		cleanup()
		return 0, 0, 0, "", "", nil, err
	}
	return stdin, stdout, stderr, stdoutPath, stderrPath, cleanup, nil
}

func createAgentJob(cfg AgentConfig) (syscall.Handle, error) {
	h, _, e1 := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return 0, e1
	}
	job := syscall.Handle(h)
	info := jobExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags =
		jobObjectLimitKillOnJobClose | jobObjectLimitActiveProcess |
			jobObjectLimitProcessMemory | jobObjectLimitJobMemory | jobObjectLimitJobTime
	info.BasicLimitInformation.ActiveProcessLimit = uint32(cfg.ProcessLimit)
	info.BasicLimitInformation.PerJobUserTimeLimit = int64(cfg.TimeoutSeconds) * 10_000_000
	info.ProcessMemoryLimit = uintptr(cfg.MemoryMB) << 20
	info.JobMemoryLimit = uintptr(cfg.MemoryMB) << 20
	r1, _, e1 := procSetInformationJobObject.Call(
		uintptr(job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
	)
	if r1 == 0 {
		syscall.CloseHandle(job)
		return 0, e1
	}
	return job, nil
}

func verifyAppContainerToken(process syscall.Handle, expectedSID string) (string, error) {
	var token syscall.Handle
	r1, _, e1 := procOpenProcessToken.Call(uintptr(process), tokenQuery, uintptr(unsafe.Pointer(&token)))
	if r1 == 0 {
		return "", e1
	}
	defer syscall.CloseHandle(token)
	var isContainer uint32
	var returned uint32
	r1, _, e1 = procGetTokenInformation.Call(
		uintptr(token), tokenIsAppContainer, uintptr(unsafe.Pointer(&isContainer)),
		unsafe.Sizeof(isContainer), uintptr(unsafe.Pointer(&returned)),
	)
	if r1 == 0 {
		return "", e1
	}
	if isContainer == 0 {
		return "", errors.New("child token is not AppContainer")
	}
	var needed uint32
	procGetTokenInformation.Call(uintptr(token), tokenAppContainerSid, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 4096 {
		return "", errors.New("AppContainer token SID size invalid")
	}
	buf := make([]byte, needed)
	r1, _, e1 = procGetTokenInformation.Call(
		uintptr(token), tokenAppContainerSid, uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed), uintptr(unsafe.Pointer(&needed)),
	)
	if r1 == 0 {
		return "", e1
	}
	sid := *(*uintptr)(unsafe.Pointer(&buf[0]))
	if sid == 0 {
		return "", errors.New("AppContainer token SID missing")
	}
	got, err := sidString(sid)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, expectedSID) {
		return got, fmt.Errorf("AppContainer SID mismatch: got %s want %s", got, expectedSID)
	}
	return got, nil
}

func readLimitedOutput(path string, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if limit < 1 {
		limit = 1 << 20
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > int64(limit) {
		buf := make([]byte, limit)
		n, _ := f.Read(buf)
		return string(buf[:n]), errors.New("agent process output exceeded limit")
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
func validateAgentWorkspaceTree(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("agent workspace contains a link")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		var data syscall.ByHandleFileInformation
		infoErr := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &data)
		_ = f.Close()
		if infoErr != nil {
			return infoErr
		}
		if data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("agent workspace contains a reparse point")
		}
		if info.Mode().IsRegular() && data.NumberOfLinks > 1 {
			return errors.New("agent workspace contains a hard-linked file")
		}
		return nil
	})
}

func (r *windowsAgentRunner) Run(ctx context.Context, req agentRunRequest) (agentRunResult, error) {
	var result agentRunResult
	if req.OwnerID == "" || req.ProjectID == "" || !filepath.IsAbs(req.Executable) || !filepath.IsAbs(req.Dir) {
		return result, errors.New("invalid agent run request")
	}
	info, err := os.Stat(req.Dir)
	if err != nil || !info.IsDir() {
		return result, errors.New("agent work directory unavailable")
	}
	if err = validateAgentWorkspaceTree(req.Dir); err != nil {
		return result, err
	}
	sid, sidText, err := appContainerProfile(req.OwnerID)
	if err != nil {
		return result, err
	}
	defer procFreeSid.Call(sid)
	if err = appContainerACL(req.Dir, sidText, "M"); err != nil {
		return result, err
	}
	if err = prepareAppContainerEnvironment(req.Env, req.OwnerID, sidText); err != nil {
		return result, err
	}
	grantedRead := []string{}
	defer func() {
		for _, p := range grantedRead {
			removeAppContainerACL(p, sidText)
		}
	}()
	for _, p := range req.ReadOnlyPaths {
		p = filepath.Clean(p)
		if !filepath.IsAbs(p) {
			return result, errors.New("agent read-only path must be absolute")
		}
		if err = appContainerACL(p, sidText, "RX"); err != nil {
			return result, err
		}
		grantedRead = append(grantedRead, p)
	}
	result.AppContainerSID = sidText

	stdin, stdout, stderr, stdoutPath, stderrPath, cleanupIO, err := createAgentIO(req.Dir)
	if err != nil {
		return result, err
	}
	defer cleanupIO()
	closeStd := func() {
		if stdin != 0 {
			syscall.CloseHandle(stdin)
			stdin = 0
		}
		if stdout != 0 {
			syscall.CloseHandle(stdout)
			stdout = 0
		}
		if stderr != 0 {
			syscall.CloseHandle(stderr)
			stderr = 0
		}
	}
	defer closeStd()
	job, err := createAgentJob(r.cfg)
	if err != nil {
		return result, err
	}
	defer syscall.CloseHandle(job)

	attrBuf, attrList, err := createAttributeList(2)
	if err != nil {
		return result, err
	}
	defer procDeleteProcThreadAttributeList.Call(attrList)
	caps := securityCapabilities{AppContainerSid: sid}
	if err = updateAttribute(attrList, procThreadAttributeSecurityCapabilities,
		uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); err != nil {
		return result, err
	}
	handles := []syscall.Handle{stdin, stdout, stderr}
	if err = updateAttribute(attrList, procThreadAttributeHandleList,
		uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return result, err
	}

	var envPtr *uint16
	var envBlock []uint16
	if req.Env != nil {
		envPtr, envBlock, err = buildWindowsEnvironment(req.Env, req.Dir)
		if err != nil {
			return result, err
		}
	}
	cmdPtr, cmdBlock, err := commandLine(req.Executable, req.Args)
	if err != nil {
		return result, err
	}
	exePtr, err := syscall.UTF16PtrFromString(req.Executable)
	if err != nil {
		return result, err
	}
	dirPtr, err := syscall.UTF16PtrFromString(req.Dir)
	if err != nil {
		return result, err
	}
	si := startupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = startfUseStdHandles
	si.StdInput, si.StdOutput, si.StdErr = stdin, stdout, stderr
	si.AttributeList = attrList
	var pi syscall.ProcessInformation

	flags := uint32(extendedStartupInfoPresent | createSuspended | createUnicodeEnvironment)
	err = syscall.CreateProcess(
		exePtr, cmdPtr, nil, nil, true, flags, envPtr, dirPtr,
		(*syscall.StartupInfo)(unsafe.Pointer(&si)), &pi,
	)
	runtime.KeepAlive(attrBuf)
	runtime.KeepAlive(caps)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(envBlock)
	runtime.KeepAlive(cmdBlock)
	if err != nil {
		return result, fmt.Errorf("CreateProcess AppContainer: %w", err)
	}
	closeStd()
	defer syscall.CloseHandle(pi.Thread)
	defer syscall.CloseHandle(pi.Process)

	gotSID, err := verifyAppContainerToken(pi.Process, sidText)
	if err != nil {
		_ = syscall.TerminateProcess(pi.Process, 126)
		return result, err
	}
	result.IsAppContainer = true
	result.AppContainerSID = gotSID

	r1, _, e1 := procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process))
	if r1 == 0 {
		_ = syscall.TerminateProcess(pi.Process, 126)
		return result, fmt.Errorf("AssignProcessToJobObject: %w", e1)
	}
	if resumed, _, e1 := procResumeThread.Call(uintptr(pi.Thread)); resumed == 0xFFFFFFFF {
		procTerminateJobObject.Call(uintptr(job), 126)
		return result, fmt.Errorf("ResumeThread: %w", e1)
	}
	cancelled := false
	timeLimited := false
	deadline := time.Now().Add(time.Duration(r.cfg.TimeoutSeconds) * time.Second)
	for {
		r1, _, e1 = procWaitForSingleObject.Call(uintptr(pi.Process), 100)
		switch r1 {
		case waitObject0:
			goto finished
		case waitTimeout:
			if !time.Now().Before(deadline) {
				timeLimited = true
				procTerminateJobObject.Call(uintptr(job), 124)
				_, _, _ = procWaitForSingleObject.Call(uintptr(pi.Process), 5000)
				goto finished
			}
			select {
			case <-ctx.Done():
				cancelled = true
				procTerminateJobObject.Call(uintptr(job), 125)
				_, _, _ = procWaitForSingleObject.Call(uintptr(pi.Process), 5000)
				goto finished
			default:
			}
		default:
			procTerminateJobObject.Call(uintptr(job), 126)
			return result, fmt.Errorf("WaitForSingleObject: %w", e1)
		}
	}

finished:
	var exitCode uint32
	r1, _, e1 = procGetExitCodeProcess.Call(uintptr(pi.Process), uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 {
		return result, fmt.Errorf("GetExitCodeProcess: %w", e1)
	}
	result.ExitCode = exitCode
	outText, outErr := readLimitedOutput(stdoutPath, r.cfg.OutputMaxBytes)
	errText, errErr := readLimitedOutput(stderrPath, r.cfg.OutputMaxBytes)
	result.Stdout, result.Stderr = outText, errText
	if outErr != nil {
		return result, outErr
	}
	if errErr != nil {
		return result, errErr
	}
	if timeLimited {
		return result, errors.New("agent process time limit exceeded")
	}
	if cancelled {
		return result, context.Canceled
	}
	return result, nil
}

func prepareAppContainerEnvironment(env map[string]string, ownerID, sidText string) error {
	if env == nil {
		return errors.New("agent environment is required")
	}
	local := strings.TrimSpace(env["LOCALAPPDATA"])
	if local == "" || !filepath.IsAbs(local) {
		return errors.New("agent LOCALAPPDATA must be an absolute path")
	}
	root := filepath.Join(local, "Packages", strings.ToLower(appContainerName(ownerID)))
	for _, rel := range []string{
		"AC\\Temp", "LocalState", "RoamingState", "Settings", "TempState",
	} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0700); err != nil {
			return err
		}
	}
	return appContainerACL(root, sidText, "M")
}
