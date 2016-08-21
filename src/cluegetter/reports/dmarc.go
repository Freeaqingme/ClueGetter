package reports

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"github.com/Freeaqingme/dmarcaggparser/dmarc"
)

func (m *module) parseDmarcMessage(msg *mail.Message) bool {
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		m.Log().Error("Could not parse Content Type: %s", err)
		return false
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		return m.parseDmarcMessageMultipart(msg, params)
	}

	body, err := ioutil.ReadAll(msg.Body)
	if err != nil {
		m.Log().Errorf("Couldn't read body: %s", err.Error())
		return false
	}
	return m.parseDmarcInner(msg.Header.Get("Content-Type"), string(body))
}

func (m *module) parseDmarcMessageMultipart(msg *mail.Message, params map[string]string) bool {
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return false
		}
		if err != nil {
			m.Log().Errorf("Coult not navigate through multipart message: %s", err)
			return false
		}
		slurp, err := ioutil.ReadAll(p)
		if err != nil {
			m.Log().Errorf("Could not read multipart message: %s", err)
			continue
		}

		if strings.Contains(p.Header.Get("Content-Type"), "text/plain") {
			continue
		}

		if m.parseDmarcInner(p.Header.Get("Content-Type"), string(slurp)) {
			return true
		}
	}

	return false
}

func (m *module) parseDmarcInner(contentType string, slurp string) bool {
	slurpString := strings.Replace(strings.TrimSpace(slurp), "\r\n", "", -1)
	// We don't try to parse the entire pesky mime multipart message.
	// Instead we check if we can simply decode it, and parse the xml
	body, err := base64.StdEncoding.DecodeString(slurpString)

	if err != nil {
		return false
	}

	files := make([]io.ReadCloser, 0)
	switch {
	case strings.Contains(contentType, "gzip"):
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return false
		}
		files = append(files, r)

	case strings.Contains(contentType, "zip"):
		r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return false
		}
		for _, f := range r.File {
			rc, err := f.Open()
			if err != nil {
				m.Log().Error(err.Error())
				continue
			}

			files = append(files, rc)
		}
	default:
		m.Log().Debug("Unknown content-type: %s", contentType)
		return false
	}

	for _, rc := range files {
		report, err := dmarc.ParseReader(rc)
		if err != nil {
			fmt.Println("Err! ", err.Error())
		}
		rc.Close()

		for _, module := range m.Modules() {
			module.DmarcReportPersist(report)
		}
	}

	if len(files) > 0 {
		return true
	}

	return false
}
