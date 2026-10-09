package main

// 기존 서비스 계정과 SCM에 저장된 자격증명은 업데이트로 덮어쓰지 않는다.
func serviceRegistrationArgs(existed bool, binary string) []string {
	action := "config"
	if !existed {
		action = "create"
	}
	args := []string{action, serviceName, "binPath=", binary, "start=", "delayed-auto", "DisplayName=", "OnTech Chat Server"}
	if !existed {
		args = append(args, "obj=", "NT AUTHORITY\\LocalService")
	}
	return args
}
