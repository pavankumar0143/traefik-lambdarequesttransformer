package lambdarequesttransformer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Config holds the plugin configuration (no configurable fields in this plugin).
type Config struct{}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{}
}

// RequestTransformer is the middleware that will modify requests.
type LambdaRequestTransformer struct {
	next http.Handler
	name string
}

// New initializes the plugin instance.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	// No config fields to validate in this plugin.
	return &LambdaRequestTransformer{
		next: next,
		name: name,
	}, nil
}

// ServeHTTP is called for each request. It transforms the request and forwards it.
func (rt *LambdaRequestTransformer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Save original details
	origMethod := req.Method
	origPath := req.URL.Path
	origQuery := req.URL.RawQuery
	origHost := req.Host

	// Copy all incoming headers into a map (combine multiple values by comma).
	headersMap := make(map[string]string)
	for h, values := range req.Header {
		headersMap[h] = strings.Join(values, ",")
	}

	// Determine client source IP
	clientIP := ""
	if ip, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		clientIP = ip
	} else {
		clientIP = req.RemoteAddr
	}

	// Get User-Agent and x-session-id (if any)
	userAgent := req.Header.Get("User-Agent")
	sessionID := req.Header.Get("x-session-id")
	identitySrc := []string{}
	if sessionID != "" {
		identitySrc = append(identitySrc, sessionID)
	}

	// Parse host into domain name and prefix (subdomain)
	domainName := origHost
	domainPrefix := ""
	if colonIdx := strings.Index(origHost, ":"); colonIdx != -1 {
		domainName = origHost[:colonIdx] // remove port if present
	}
	parts := strings.Split(domainName, ".")
	if len(parts) > 1 {
		domainPrefix = parts[0]
	} else {
		domainPrefix = domainName
	}

	// Generate a unique request ID (UUIDv4)
	requestID := generateUUID()

	// Timestamp (ISO 8601) and epoch milliseconds
	now := time.Now().UTC()
	timeStr := now.Format(time.RFC3339)
	timeEpoch := now.UnixNano() / 1e6

	// Construct the JSON event body
	event := map[string]interface{}{
		"version":        "2.0",
		"type":           "REQUEST",
		"routeKey":       fmt.Sprintf("%s %s", origMethod, origPath),
		"rawPath":        origPath,
		"rawQueryString": origQuery,
		"headers":        headersMap,
		"requestContext": map[string]interface{}{
			"accountId":    "local",
			"apiId":        "local",
			"domainName":   domainName,
			"domainPrefix": domainPrefix,
			"http": map[string]interface{}{
				"method":    origMethod,
				"path":      origPath,
				"protocol":  req.Proto, // e.g. "HTTP/1.1"
				"sourceIp":  clientIP,
				"userAgent": userAgent,
			},
			"requestId": requestID,
			"routeKey":  fmt.Sprintf("%s %s", origMethod, origPath),
			"stage":     "local",
			"time":      timeStr,
			"timeEpoch": timeEpoch,
		},
		"body":            "",
		"isBase64Encoded": false,
		"identitySource":  identitySrc,
	}

	// Serialize the event to JSON
	jsonData, err := json.Marshal(event)
	if err != nil {
		http.Error(rw, "JSON marshal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Replace the request body with the JSON payload
	req.Body = io.NopCloser(strings.NewReader(string(jsonData)))
	req.ContentLength = int64(len(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(jsonData)))
	req.Method = http.MethodPost // Override method to POST
	req.TransferEncoding = nil   // Disable chunked transfer if it was set

	// Set URL path to Lambda invocation format
	req.RequestURI = "/2015-03-31/functions/function/invocations"

	// Call the next handler (forward to the upstream service)
	rt.next.ServeHTTP(rw, req)
}

// generateUUID creates a random UUID v4 string.
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback: use current time to generate pseudo-unique bytes
		t := time.Now().UnixNano()
		for i := 0; i < 16; i++ {
			b[i] = byte(t >> (i * 8))
		}
	}
	// Set UUID version (4) and variant (RFC 4122)
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
