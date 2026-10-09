package server

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type imapResult struct {
	lines    []string
	literals [][]byte
}
type imapClient struct {
	conn     net.Conn
	r        *bufio.Reader
	sequence int
}

var literalSize = regexp.MustCompile(`\{([0-9]+)\+?\}$`)
var uidValidity = regexp.MustCompile(`(?i)\bUIDVALIDITY\s+([0-9]+)\b`)
var internalDate = regexp.MustCompile(`(?i)INTERNALDATE "([^"]+)"`)
var rfcSize = regexp.MustCompile(`(?i)RFC822\.SIZE ([0-9]+)`)
var fetchUID = regexp.MustCompile(`(?i)\bUID ([0-9]+)`)

func imapQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n\x00") {
		return "", errors.New("IMAP value contains control characters")
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`, nil
}
func modifiedUTF7(s string) string {
	var out strings.Builder
	var pending []rune
	flush := func() {
		if len(pending) == 0 {
			return
		}
		u := utf16.Encode(pending)
		b := make([]byte, len(u)*2)
		for i, v := range u {
			binary.BigEndian.PutUint16(b[i*2:], v)
		}
		out.WriteByte('&')
		out.WriteString(strings.ReplaceAll(base64.RawStdEncoding.EncodeToString(b), "/", ","))
		out.WriteByte('-')
		pending = nil
	}
	for _, r := range s {
		if r >= 0x20 && r <= 0x7e {
			flush()
			if r == '&' {
				out.WriteString("&-")
			} else {
				out.WriteRune(r)
			}
		} else {
			pending = append(pending, r)
		}
	}
	flush()
	return out.String()
}
func (c *imapClient) line() (string, error) {
	var b strings.Builder
	for {
		part, prefix, e := c.r.ReadLine()
		if e != nil {
			return "", e
		}
		if b.Len()+len(part) > 1<<20 {
			return "", errors.New("IMAP response line too long")
		}
		b.Write(part)
		if !prefix {
			return b.String(), nil
		}
	}
}
func (c *imapClient) command(command string) (imapResult, error) {
	c.sequence++
	tag := fmt.Sprintf("A%04d", c.sequence)
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, e := fmt.Fprintf(c.conn, "%s %s\r\n", tag, command); e != nil {
		return imapResult{}, errors.New("IMAP write failed")
	}
	out := imapResult{}
	total := 0
	for {
		line, e := c.line()
		if e != nil {
			return out, errors.New("IMAP response interrupted")
		}
		total += len(line)
		if total > 4<<20 || len(out.lines) > 10000 {
			return out, errors.New("IMAP response limit")
		}
		if strings.HasPrefix(line, tag+" ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[1], "OK") {
				// 일부 IMAP 서버(네이버 포함)는 UIDVALIDITY 같은 응답 코드를
				// tagged OK 완료 줄에만 넣을 수 있으므로 호출자가 검사할 수 있게 보존합니다.
				out.lines = append(out.lines, line)
				return out, nil
			}
			return out, errors.New("IMAP command rejected")
		}
		if strings.HasPrefix(strings.ToUpper(line), "* BYE") {
			return out, errors.New("IMAP connection closed")
		}
		out.lines = append(out.lines, line)
		if m := literalSize.FindStringSubmatch(line); len(m) == 2 {
			n, e := strconv.Atoi(m[1])
			if e != nil || n > maxRawMail+1 || n < 0 {
				return out, errMailTooLarge
			}
			total += n
			if total > 4<<20 {
				return out, errMailTooLarge
			}
			b := make([]byte, n)
			if _, e = io.ReadFull(c.r, b); e != nil {
				return out, errors.New("IMAP literal interrupted")
			}
			out.literals = append(out.literals, b)
		}
	}
}

// 평상시 메일 수집은 JSON에서 복호화한 값만 사용합니다. INI/환경 변수는 한 번만 이전합니다.
func readMailCredentials(c MailConfig) (string, string, error) {
	if c.Username == "" || c.password == "" {
		return "", "", errors.New("JSON mail credentials missing; open server settings")
	}
	return c.Username, c.password, nil
}
func (a *App) setMailState(s string) {
	a.stateMu.Lock()
	a.mailState = s
	a.mailChecked = now()
	a.stateMu.Unlock()
}
func (a *App) mailLoop() {
	if !a.cfg.Mail.Enabled {
		return
	}
	ticker := time.NewTicker(time.Duration(a.cfg.Mail.PollSeconds) * time.Second)
	defer ticker.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		if e := a.pollMail(); e != nil {
			a.setMailState(e.Error())
		}
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) pollMail() error {
	_, e := a.runMail(a.ctx, a.cfg.Mail, false, nil)
	return e
}
func (a *App) consumeMail(key string, validity, uid uint32, p *ParsedAuthMail, received time.Time) error {
	_, _, e := a.deliverMail(a.cfg.Mail, key, validity, uid, p, received, true)
	return e
}

// 수동 재검사는 이미 지나간 UID도 확인하지만 수신 커서를 되돌리거나 앞당기지 않습니다.
func (a *App) deliverMail(cfg MailConfig, key string, validity, uid uint32, p *ParsedAuthMail, received time.Time, advance bool) (int, int, error) {
	rooms := []string{}
	duplicates := 0
	e := a.store.update(func(st *State) error {
		old := st.MailCursors[key]
		if advance && old.Validity == validity && old.UID >= uid {
			return errUnchanged
		}
		if p != nil {
			if st.MailDelivered == nil {
				st.MailDelivered = map[string]int64{}
			}
			for k, until := range st.MailDelivered {
				if until <= now() {
					delete(st.MailDelivered, k)
				}
			}
			b, e := jsonMarshalAuth(p, received)
			if e != nil {
				return e
			}
			eligible := 0
			for _, name := range mailRecipients(cfg) {
				u, ok := findUsername(st, name)
				if !ok || u.Disabled {
					continue
				}
				eligible++
				room := authRoomID(u.ID)
				r, exists := st.Rooms[room]
				if !exists || r.Kind != "auth" || r.OwnerID != u.ID || len(r.Members) != 1 {
					return errors.New("private inbox invalid")
				}
				if _, member := r.Members[u.ID]; !member {
					return errors.New("private inbox membership invalid")
				}
				clientID := fmt.Sprintf("mail-%s-%d-%d", digest(key)[:12], validity, uid)
				deliveryKey := clientID + "-" + u.ID
				duplicate := st.MailDelivered[deliveryKey] > now()
				if !duplicate {
					for _, m := range st.Messages {
						if m.RoomID == room && m.ClientID == clientID {
							duplicate = true
							break
						}
					}
				}
				if duplicate {
					duplicates++
					continue
				}
				if len(st.MailDelivered) >= 20000 {
					return errors.New("mail delivery ledger capacity reached")
				}
				_, e = addMessage(st, Message{RoomID: room, Username: "Naver Mail", Kind: "auth", Text: b, ClientID: clientID, Expires: now() + int64(a.cfg.AuthRetentionMinutes)*60}, a.cfg.MaxMessages, a.cfg.FCM.Enabled)
				if e != nil {
					return e
				}
				st.MailDelivered[deliveryKey] = now() + 48*3600
				rooms = append(rooms, room)
			}
			if eligible == 0 {
				return errors.New("no enabled mail recipients")
			}
		}
		if advance {
			st.MailCursors[key] = MailCursor{Validity: validity, UID: uid}
		}
		return nil
	})
	if e != nil {
		return 0, 0, e
	}
	for _, room := range rooms {
		a.changed(room)
	}
	return len(rooms), duplicates, nil
}

func jsonMarshalAuth(p *ParsedAuthMail, t time.Time) (string, error) {
	b, e := json.Marshal(map[string]any{"service": p.Service, "subject": p.Subject, "body": p.Body, "received_at": t.Unix()})
	return string(b), e
}
