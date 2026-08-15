package eis

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client — SOAP-клиент getDocs* ЕИС (44-ФЗ).
type Client struct {
	endpoint   string
	token      string // UUID / код ИС → index.sender
	mode       string
	httpClient *http.Client
	profile    string
}

type ClientOptions struct {
	Endpoint   string
	Token      string
	Mode       string
	Profile    string
	TLSCert    string
	TLSKey     string
	CACert     string
	SkipVerify bool
	Timeout    time.Duration
}

func NewClient(opts ClientOptions) (*Client, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("eis endpoint is required")
	}
	if opts.Token == "" {
		return nil, fmt.Errorf("eis token (UUID) is required")
	}
	if opts.Mode == "" {
		opts.Mode = "PROD"
	}
	if opts.Profile == "" {
		opts.Profile = "mis"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 120 * time.Second
	}

	tlsCfg := &tls.Config{InsecureSkipVerify: opts.SkipVerify} //nolint:gosec // configurable for test envs
	if opts.TLSCert != "" && opts.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(opts.TLSCert, opts.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	if opts.CACert != "" {
		pem, err := os.ReadFile(opts.CACert)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse ca cert")
		}
		tlsCfg.RootCAs = pool
	}

	return &Client{
		endpoint: opts.Endpoint,
		token:    opts.Token,
		mode:     opts.Mode,
		profile:  opts.Profile,
		httpClient: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		},
	}, nil
}

// RegionRequest — параметры getPublicDocsByOrgRegion.
type RegionRequest struct {
	OrgRegion    string
	Subsystem    string
	DocumentType string
	FromHour     int
	ToHour       int
	Timezone     int
	AllOrgs      bool
}

// DocsResponse — результат SOAP-запроса.
type DocsResponse struct {
	NoData      bool
	ArchiveURLs []string
	RawXML      string
}

func (c *Client) GetPublicDocsByOrgRegion(ctx context.Context, req RegionRequest) (*DocsResponse, error) {
	body := buildGetPublicDocsByOrgRegion(c.token, c.mode, req)
	raw, err := c.doSOAP(ctx, body)
	if err != nil {
		return nil, err
	}
	return parseDocsResponse(raw)
}

func (c *Client) GetPreparedPart(ctx context.Context) (*DocsResponse, error) {
	body := buildGetPreparedPart(c.token, c.mode)
	raw, err := c.doSOAP(ctx, body)
	if err != nil {
		return nil, err
	}
	return parseDocsResponse(raw)
}

func (c *Client) DownloadArchive(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Токен пользователя передаём и в заголовке (на случай альтернативной HTTPS-интеграции).
	req.Header.Set("X-EIS-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download archive: status %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) doSOAP(ctx context.Context, body string) (string, error) {
	envelope := soapEnvelope(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(envelope))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", soapAction(c.profile))
	req.Header.Set("X-EIS-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("soap call: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("soap status %d: %s", resp.StatusCode, truncate(string(raw), 512))
	}
	if fault := extractFault(raw); fault != "" {
		return "", fmt.Errorf("soap fault: %s", fault)
	}
	return string(raw), nil
}

func soapAction(profile string) string {
	switch strings.ToLower(profile) {
	case "ip":
		return "http://zakupki.gov.ru/fz44/queue/ws/get-docs-ip"
	case "le":
		return "http://zakupki.gov.ru/fz44/queue/ws/get-docs-le"
	case "org":
		return "http://zakupki.gov.ru/fz44/queue/ws/get-docs-org"
	case "ris":
		return "http://zakupki.gov.ru/fz44/ws/get-docs-ris"
	default:
		return "http://zakupki.gov.ru/fz44/ws/get-docs-mis"
	}
}

func soapEnvelope(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<soapenv:Header/><soapenv:Body>` + inner + `</soapenv:Body></soapenv:Envelope>`
}

func buildGetPublicDocsByOrgRegion(token, mode string, req RegionRequest) string {
	ns := "http://zakupki.gov.ru/fz44/get-docs-mis/ws"
	allOrgs := "true"
	if !req.AllOrgs {
		allOrgs = "false"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<mis:getPublicDocsByOrgRegionRequest xmlns:mis="%s">`, ns)
	fmt.Fprintf(&b, `<index><id>%s</id><sender>%s</sender><mode>%s</mode></index>`,
		xmlEscape(newRequestID()), xmlEscape(token), xmlEscape(mode))
	fmt.Fprintf(&b, `<selectionParams44>`)
	fmt.Fprintf(&b, `<orgRegion>%s</orgRegion>`, xmlEscape(req.OrgRegion))
	fmt.Fprintf(&b, `<subsystemType>%s</subsystemType>`, xmlEscape(req.Subsystem))
	fmt.Fprintf(&b, `<documentType>%s</documentType>`, xmlEscape(req.DocumentType))
	fmt.Fprintf(&b, `<periodInfo><todayInfo><fromHour>%d</fromHour><toHour>%d</toHour><offsetTimeZone>%d</offsetTimeZone></todayInfo></periodInfo>`,
		req.FromHour, req.ToHour, req.Timezone)
	if req.AllOrgs {
		fmt.Fprintf(&b, `<isAllOrganizations44>%s</isAllOrganizations44>`, allOrgs)
	}
	fmt.Fprintf(&b, `</selectionParams44></mis:getPublicDocsByOrgRegionRequest>`)
	return b.String()
}

func buildGetPreparedPart(token, mode string) string {
	ns := "http://zakupki.gov.ru/fz44/get-docs-mis/ws"
	return fmt.Sprintf(
		`<mis:getPreparedPartRequest xmlns:mis="%s"><index><id>%s</id><sender>%s</sender><mode>%s</mode></index></mis:getPreparedPartRequest>`,
		ns, xmlEscape(newRequestID()), xmlEscape(token), xmlEscape(mode),
	)
}

func parseDocsResponse(raw string) (*DocsResponse, error) {
	out := &DocsResponse{RawXML: raw}
	if strings.Contains(raw, "<noData>true</noData>") || strings.Contains(raw, "<noData>1</noData>") {
		out.NoData = true
		return out, nil
	}
	urls := extractTagValues(raw, "archiveUrl")
	out.ArchiveURLs = urls
	return out, nil
}

func extractTagValues(xmlStr, localName string) []string {
	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != localName {
			continue
		}
		var v string
		if err := dec.DecodeElement(&v, &se); err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func extractFault(raw []byte) string {
	s := string(raw)
	if !strings.Contains(s, "Fault") {
		return ""
	}
	vals := extractTagValues(s, "faultstring")
	if len(vals) > 0 {
		return vals[0]
	}
	vals = extractTagValues(s, "faultString")
	if len(vals) > 0 {
		return vals[0]
	}
	return "unknown SOAP fault"
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func newRequestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
