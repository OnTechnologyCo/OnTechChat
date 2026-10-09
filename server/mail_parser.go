package server

import (
	"bytes"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"unicode/utf8"
)

const maxRawMail = 2 << 20
const maxDecodedMail = 256 << 10

type ParsedAuthMail struct {
	Service string `json:"service"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

var errMailTooLarge = errors.New("mail size limit exceeded")

func decodeHeader(s string) (string, error) {
	d := mime.WordDecoder{CharsetReader: charsetReader}
	return d.DecodeHeader(s)
}
func charsetReader(name string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return r, nil
	case "iso-8859-1", "latin1":
		b, e := io.ReadAll(io.LimitReader(r, maxDecodedMail+1))
		if e != nil || len(b) > maxDecodedMail {
			return nil, errMailTooLarge
		}
		var out strings.Builder
		for _, c := range b {
			out.WriteRune(rune(c))
		}
		return strings.NewReader(out.String()), nil
	}
	return decodeAdditionalMailCharset(name, r)
}

// HTML은 실행하지 않고 화면에 보이는 텍스트만 추출합니다.
// href/src/style/script/head 같은 코드·URL 속성은 본문에 포함하지 않습니다.
func htmlText(s string) string {
	var b strings.Builder
	skip := ""
	block := func(tag string) bool {
		switch tag {
		case "br", "p", "/p", "div", "/div", "li", "/li", "tr", "/tr", "table", "/table",
			"section", "/section", "article", "/article", "h1", "/h1", "h2", "/h2", "h3", "/h3",
			"h4", "/h4", "h5", "/h5", "h6", "/h6":
			return true
		}
		return false
	}
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				break
			}
			i += end + 7
			continue
		}
		if s[i] == '<' {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				break
			}
			raw := strings.ToLower(strings.TrimSpace(s[i+1 : i+end]))
			name := strings.Fields(raw)
			if len(name) > 0 {
				t := strings.TrimSuffix(name[0], "/")
				if t == "script" || t == "style" || t == "head" {
					skip = t
				}
				if skip != "" && t == "/"+skip {
					skip = ""
				}
				if skip == "" && block(t) {
					b.WriteByte('\n')
				} else if skip == "" {
					b.WriteByte(' ')
				}
			}
			i += end + 1
			continue
		}
		if skip == "" {
			b.WriteByte(s[i])
		}
		i++
	}
	text := html.UnescapeString(b.String())
	lines := []string{}
	blank := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line = strings.Join(strings.Fields(strings.ReplaceAll(line, "\u00a0", " ")), " ")
		if line == "" {
			if !blank && len(lines) > 0 {
				lines = append(lines, "")
				blank = true
			}
			continue
		}
		lines = append(lines, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
func mailBodies(h textproto.MIMEHeader, r io.Reader, depth int, budget *int, parts *int) ([]string, error) {
	*parts++
	if depth > 8 || *parts > 40 {
		return nil, errors.New("MIME nesting limit")
	}
	disposition, _, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	if strings.EqualFold(disposition, "attachment") {
		return nil, nil
	}
	media, params, e := mime.ParseMediaType(h.Get("Content-Type"))
	if h.Get("Content-Type") == "" {
		media = "text/plain"
		params = map[string]string{}
	} else if e != nil {
		return nil, errors.New("invalid MIME content type")
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "", "7bit", "8bit", "binary":
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	default:
		return nil, errors.New("unsupported mail transfer encoding")
	}
	if strings.HasPrefix(media, "multipart/") {
		if params["boundary"] == "" {
			return nil, errors.New("MIME boundary missing")
		}
		mr := multipart.NewReader(r, params["boundary"])
		out := []string{}
		htmlAlternative := []string{}
		plainAlternative := []string{}
		otherAlternative := []string{}
		alternative := strings.EqualFold(media, "multipart/alternative")
		startBudget := *budget
		for {
			p, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, errors.New("invalid multipart body")
			}
			childMedia, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
			childBudget := *budget
			if alternative {
				// 대체 표현은 하나만 선택하므로 각 표현을 같은 예산으로 독립 디코딩합니다.
				// 긴 text/plain 추적 URL이 뒤의 HTML 본문 예산을 소모하지 않게 합니다.
				childBudget = startBudget
			}
			texts, e := mailBodies(p.Header, p, depth+1, &childBudget, parts)
			p.Close()
			if e != nil {
				return nil, e
			}
			if !alternative {
				*budget = childBudget
				out = append(out, texts...)
				continue
			}
			switch {
			case strings.EqualFold(childMedia, "text/html"), strings.HasPrefix(strings.ToLower(childMedia), "multipart/related"):
				htmlAlternative = append(htmlAlternative, texts...)
			case strings.EqualFold(childMedia, "text/plain"), childMedia == "":
				plainAlternative = append(plainAlternative, texts...)
			default:
				otherAlternative = append(otherAlternative, texts...)
			}
		}
		if alternative {
			// HTML 대체 본문이 있으면 실제 화면에 보이는 HTML 텍스트만 사용합니다.
			// 같은 내용을 포함하는 text/plain 추적 URL 버전을 함께 붙이지 않습니다.
			selected := htmlAlternative
			if len(selected) == 0 {
				selected = plainAlternative
			}
			if len(selected) == 0 {
				selected = otherAlternative
			}
			used := 0
			for _, text := range selected {
				used += len(text)
			}
			if used > startBudget {
				return nil, errMailTooLarge
			}
			*budget = startBudget - used
			return selected, nil
		}
		return out, nil
	}
	if media != "text/plain" && media != "text/html" {
		return nil, nil
	}
	r, e = charsetReader(params["charset"], r)
	if e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(r, int64(*budget)+1))
	if e != nil {
		return nil, errors.New("mail body decoding failed")
	}
	if len(b) > *budget {
		return nil, errMailTooLarge
	}
	*budget -= len(b)
	if !utf8.Valid(b) {
		return nil, errors.New("invalid UTF-8 mail")
	}
	s := string(b)
	if media == "text/html" {
		s = htmlText(s)
	}
	return []string{s}, nil
}
func ParseAuthMail(raw []byte, cfg MailConfig) (*ParsedAuthMail, error) {
	if len(raw) > maxRawMail {
		return nil, errMailTooLarge
	}
	m, e := mail.ReadMessage(bytes.NewReader(raw))
	if e != nil {
		return nil, errors.New("invalid mail message")
	}

	subject, e := decodeHeader(m.Header.Get("Subject"))
	if e != nil {
		return nil, errors.New("메일 제목 문자 인코딩을 해석하지 못했습니다.")
	}
	if !mailSubjectMatches(subject, cfg) {
		return nil, nil
	}
	parsed := &ParsedAuthMail{Service: "Naver Mail", Subject: strings.TrimSpace(subject)}
	budget, parts := maxDecodedMail, 0
	texts, e := mailBodies(textproto.MIMEHeader(m.Header), m.Body, 0, &budget, &parts)
	if e != nil {
		return parsed, e
	}
	seen := map[string]bool{}
	body := []string{}
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text != "" && !seen[text] {
			body = append(body, text)
			seen[text] = true
		}
	}
	parsed.Body = strings.Join(body, "\n\n")
	return parsed, nil
}
