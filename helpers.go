// ⚡️ Fiber is an Express inspired web framework written in Go with ☕️
// 🤖 Github Repository: https://github.com/gofiber/fiber
// 📌 API Documentation: https://docs.gofiber.io

package fiber

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/gofiber/fiber/v3/log"
	"github.com/gofiber/utils/v2"

	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

// acceptedType is a struct that holds the parsed value of an Accept header
// along with quality, specificity, parameters, and order.
// Used for sorting accept headers.
type acceptedType struct {
	spec        string
	quality     float64
	specificity int
	order       int
	params      headerParams
}

type headerParams map[string][]byte

// getTLSConfig returns a net listener's tls config
func getTLSConfig(ln net.Listener) *tls.Config {
	// Get listener type
	pointer := reflect.ValueOf(ln)

	// Is it a tls.listener?
	if pointer.String() != "<*tls.listener Value>" {
		return nil
	}

	// Copy value from pointer
	if val := reflect.Indirect(pointer); val.Type() != nil {
		// Get private field from value
		if field := val.FieldByName("config"); field.Type() != nil {
			// Copy value from pointer field (unsafe)
			newval := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())) //nolint:gosec // Probably the only way to extract the *tls.Config from a net.Listener. TODO: Verify there really is no easier way without using unsafe.
			if newval.Type() == nil {
				return nil
			}
			// Get element from pointer
			if elem := newval.Elem(); elem.Type() != nil {
				// Cast value to *tls.Config
				c, ok := elem.Interface().(*tls.Config)
				if !ok {
					panic(errors.New("failed to type-assert to *tls.Config"))
				}
				return c
			}
		}
	}

	return nil
}

// readContent opens a named file and read content from it
func readContent(rf io.ReaderFrom, name string) (int64, error) {
	// Read file
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return 0, fmt.Errorf("failed to open: %w", err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			log.Errorf("Error closing file: %s", err)
		}
	}()
	if n, err := rf.ReadFrom(f); err != nil {
		return n, fmt.Errorf("failed to read: %w", err)
	}
	return 0, nil
}

// quoteString escape special characters in a given string
func (app *App) quoteString(raw string) string {
	bb := bytebufferpool.Get()
	quoted := app.getString(fasthttp.AppendQuotedArg(bb.B, app.getBytes(raw)))
	bytebufferpool.Put(bb)
	return quoted
}

// Scan stack if other methods match the request
func (app *App) methodExist(c *DefaultCtx) bool {
	var exists bool

	methods := app.config.RequestMethods
	for i := 0; i < len(methods); i++ {
		// Skip original method
		if c.getMethodINT() == i {
			continue
		}
		// Reset stack index
		c.setIndexRoute(-1)

		tree, ok := c.App().treeStack[i][c.getTreePath()]
		if !ok {
			tree = c.App().treeStack[i][""]
		}
		// Get stack length
		lenr := len(tree) - 1
		// Loop over the route stack starting from previous index
		for c.getIndexRoute() < lenr {
			// Increment route index
			c.setIndexRoute(c.getIndexRoute() + 1)
			// Get *Route
			route := tree[c.getIndexRoute()]
			// Skip use routes
			if route.use {
				continue
			}
			// Check if it matches the request path
			match := route.match(c.getDetectionPath(), c.Path(), c.getValues())
			// No match, next route
			if match {
				// We matched
				exists = true
				// Add method to Allow header
				c.Append(HeaderAllow, methods[i])
				// Break stack loop
				break
			}
		}
	}
	return exists
}

// Scan stack if other methods match the request
func (app *App) methodExistCustom(c CustomCtx) bool {
	var exists bool
	methods := app.config.RequestMethods
	for i := 0; i < len(methods); i++ {
		// Skip original method
		if c.getMethodINT() == i {
			continue
		}
		// Reset stack index
		c.setIndexRoute(-1)

		tree, ok := c.App().treeStack[i][c.getTreePath()]
		if !ok {
			tree = c.App().treeStack[i][""]
		}
		// Get stack length
		lenr := len(tree) - 1
		// Loop over the route stack starting from previous index
		for c.getIndexRoute() < lenr {
			// Increment route index
			c.setIndexRoute(c.getIndexRoute() + 1)
			// Get *Route
			route := tree[c.getIndexRoute()]
			// Skip use routes
			if route.use {
				continue
			}
			// Check if it matches the request path
			match := route.match(c.getDetectionPath(), c.Path(), c.getValues())
			// No match, next route
			if match {
				// We matched
				exists = true
				// Add method to Allow header
				c.Append(HeaderAllow, methods[i])
				// Break stack loop
				break
			}
		}
	}
	return exists
}

// uniqueRouteStack drop all not unique routes from the slice
func uniqueRouteStack(stack []*Route) []*Route {
	var unique []*Route
	m := make(map[*Route]int)
	for _, v := range stack {
		if _, ok := m[v]; !ok {
			// Unique key found. Record position and collect
			// in result.
			m[v] = len(unique)
			unique = append(unique, v)
		}
	}

	return unique
}

// defaultString returns the value or a default value if it is set
func defaultString(value string, defaultValue []string) string {
	if len(value) == 0 && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return value
}

func getGroupPath(prefix, path string) string {
	if len(path) == 0 {
		return prefix
	}

	if path[0] != '/' {
		path = "/" + path
	}

	return strings.TrimRight(prefix, "/") + path
}

// acceptsOffer This function determines if an offer matches a given specification.
// It checks if the specification ends with a '*' or if the offer has the prefix of the specification.
// Returns true if the offer matches the specification, false otherwise.
func acceptsOffer(spec, offer string, _ headerParams) bool {
	if len(spec) >= 1 && spec[len(spec)-1] == '*' {
		return true
	} else if strings.HasPrefix(spec, offer) {
		return true
	}
	return false
}

// acceptsOfferType This function determines if an offer type matches a given specification.
// It checks if the specification is equal to */* (i.e., all types are accepted).
// It gets the MIME type of the offer (either from the offer itself or by its file extension).
// It checks if the offer MIME type matches the specification MIME type or if the specification is of the form <MIME_type>/* and the offer MIME type has the same MIME type.
// It checks if the offer contains every parameter present in the specification.
// Returns true if the offer type matches the specification, false otherwise.
func acceptsOfferType(spec, offerType string, specParams headerParams) bool {
	var offerMime, offerParams string

	if i := strings.IndexByte(offerType, ';'); i == -1 {
		offerMime = offerType
	} else {
		offerMime = offerType[:i]
		offerParams = offerType[i:]
	}

	// Accept: */*
	if spec == "*/*" {
		return paramsMatch(specParams, offerParams)
	}

	var mimetype string
	if strings.IndexByte(offerMime, '/') != -1 {
		mimetype = offerMime // MIME type
	} else {
		mimetype = utils.GetMIME(offerMime) // extension
	}

	if spec == mimetype {
		// Accept: <MIME_type>/<MIME_subtype>
		return paramsMatch(specParams, offerParams)
	}

	s := strings.IndexByte(mimetype, '/')
	// Accept: <MIME_type>/*
	if strings.HasPrefix(spec, mimetype[:s]) && (spec[s:] == "/*" || mimetype[s:] == "/*") {
		return paramsMatch(specParams, offerParams)
	}

	return false
}

// paramsMatch returns whether offerParams contains all parameters present in specParams.
// Matching is case insensitive, and surrounding quotes are stripped.
// To align with the behavior of res.format from Express, the order of parameters is
// ignored, and if a parameter is specified twice in the incoming Accept, the last
// provided value is given precedence.
// In the case of quoted values, RFC 9110 says that we must treat any character escaped
// by a backslash as equivalent to the character itself (e.g., "a\aa" is equivalent to "aaa").
// For the sake of simplicity, we forgo this and compare the value as-is. Besides, it would
// be highly unusual for a client to escape something other than a double quote or backslash.
// See https://www.rfc-editor.org/rfc/rfc9110#name-parameters
func paramsMatch(specParamStr headerParams, offerParams string) bool {
	if len(specParamStr) == 0 {
		return true
	}

	allSpecParamsMatch := true
	for specParam, specVal := range specParamStr {
		foundParam := false
		fasthttp.VisitHeaderParams(utils.UnsafeBytes(offerParams), func(key, value []byte) bool {
			if utils.EqualFold(specParam, string(key)) {
				foundParam = true
				allSpecParamsMatch = utils.EqualFold(specVal, value)
				return false
			}
			return true
		})
		if !foundParam || !allSpecParamsMatch {
			return false
		}
	}

	return allSpecParamsMatch
}

// getSplicedStrList function takes a string and a string slice as an argument, divides the string into different
// elements divided by ',' and stores these elements in the string slice.
// It returns the populated string slice as an output.
//
// If the given slice hasn't enough space, it will allocate more and return.
func getSplicedStrList(headerValue string, dst []string) []string {
	if headerValue == "" {
		return nil
	}

	var (
		index             int
		character         rune
		lastElementEndsAt uint8
		insertIndex       int
	)
	for index, character = range headerValue + "$" {
		if character == ',' || index == len(headerValue) {
			if insertIndex >= len(dst) {
				oldSlice := dst
				dst = make([]string, len(dst)+(len(dst)>>1)+2)
				copy(dst, oldSlice)
			}
			dst[insertIndex] = strings.TrimLeft(headerValue[lastElementEndsAt:index], " ")
			lastElementEndsAt = uint8(index + 1)
			insertIndex++
		}
	}

	if len(dst) > insertIndex {
		dst = dst[:insertIndex]
	}
	return dst
}

// forEachMediaRange parses an Accept or Content-Type header, calling functor
// on each media range.
// See: https://www.rfc-editor.org/rfc/rfc9110#name-content-negotiation-fields
func forEachMediaRange(header []byte, functor func([]byte)) {
	hasDQuote := bytes.IndexByte(header, '"') != -1

	for len(header) > 0 {
		n := 0
		header = bytes.TrimLeft(header, " ")
		quotes := 0
		escaping := false

		if hasDQuote {
			// Complex case. We need to keep track of quotes and quoted-pairs (i.e.,  characters escaped with \ )
		loop:
			for n < len(header) {
				switch header[n] {
				case ',':
					if quotes%2 == 0 {
						break loop
					}
				case '"':
					if !escaping {
						quotes++
					}
				case '\\':
					if quotes%2 == 1 {
						escaping = !escaping
					}
				}
				n++
			}
		} else {
			// Simple case. Just look for the next comma.
			if n = bytes.IndexByte(header, ','); n == -1 {
				n = len(header)
			}
		}

		functor(header[:n])

		if n >= len(header) {
			return
		}
		header = header[n+1:]
	}
}

// Pool for headerParams instances. The headerParams object *must*
// be cleared before being returned to the pool.
var headerParamPool = sync.Pool{
	New: func() any {
		return make(headerParams)
	},
}

// getOffer return valid offer for header negotiation.
// Do not pass header using utils.UnsafeBytes - this can cause a panic due
// to the use of utils.ToLowerBytes.
func getOffer(header []byte, isAccepted func(spec, offer string, specParams headerParams) bool, offers ...string) string {
	if len(offers) == 0 {
		return ""
	}
	if len(header) == 0 {
		return offers[0]
	}

	acceptedTypes := make([]acceptedType, 0, 8)
	order := 0

	// Parse header and get accepted types with their quality and specificity
	// See: https://www.rfc-editor.org/rfc/rfc9110#name-content-negotiation-fields
	forEachMediaRange(header, func(accept []byte) {
		order++
		spec, quality := accept, 1.0

		var params headerParams

		if i := bytes.IndexByte(accept, ';'); i != -1 {
			spec = accept[:i]

			// The vast majority of requests will have only the q parameter with
			// no whitespace. Check this first to see if we can skip
			// the more involved parsing.
			if bytes.HasPrefix(accept[i:], []byte(";q=")) && bytes.IndexByte(accept[i+3:], ';') == -1 {
				if q, err := fasthttp.ParseUfloat(bytes.TrimRight(accept[i+3:], " ")); err == nil {
					quality = q
				}
			} else {
				params, _ = headerParamPool.Get().(headerParams) //nolint:errcheck // only contains headerParams
				fasthttp.VisitHeaderParams(accept[i:], func(key, value []byte) bool {
					if string(key) == "q" {
						if q, err := fasthttp.ParseUfloat(value); err == nil {
							quality = q
						}
						return false
					}
					params[utils.UnsafeString(utils.ToLowerBytes(key))] = value
					return true
				})
			}

			// Skip this accept type if quality is 0.0
			// See: https://www.rfc-editor.org/rfc/rfc9110#quality.values
			if quality == 0.0 {
				return
			}
		}

		spec = bytes.TrimRight(spec, " ")

		// Get specificity
		var specificity int
		// check for wildcard this could be a mime */* or a wildcard character *
		switch {
		case string(spec) == "*/*" || string(spec) == "*":
			specificity = 1
		case bytes.HasSuffix(spec, []byte("/*")):
			specificity = 2
		case bytes.IndexByte(spec, '/') != -1:
			specificity = 3
		default:
			specificity = 4
		}

		// Add to accepted types
		acceptedTypes = append(acceptedTypes, acceptedType{utils.UnsafeString(spec), quality, specificity, order, params})
	})

	if len(acceptedTypes) > 1 {
		// Sort accepted types by quality and specificity, preserving order of equal elements
		sortAcceptedTypes(&acceptedTypes)
	}

	// Find the first offer that matches the accepted types
	ret := ""
	done := false
	for _, acceptedType := range acceptedTypes {
		if !done {
			for _, offer := range offers {
				if offer == "" {
					continue
				}
				if isAccepted(acceptedType.spec, offer, acceptedType.params) {
					ret = offer
					done = true
					break
				}
			}
		}
		if acceptedType.params != nil {
			for p := range acceptedType.params {
				delete(acceptedType.params, p)
			}
			headerParamPool.Put(acceptedType.params)
		}
	}

	return ret
}

// sortAcceptedTypes sorts accepted types by quality and specificity, preserving order of equal elements
// A type with parameters has higher priority than an equivalent one without parameters.
// e.g., text/html;a=1;b=2 comes before text/html;a=1
// See: https://www.rfc-editor.org/rfc/rfc9110#name-content-negotiation-fields
func sortAcceptedTypes(acceptedTypes *[]acceptedType) {
	if acceptedTypes == nil || len(*acceptedTypes) < 2 {
		return
	}
	at := *acceptedTypes

	for i := 1; i < len(at); i++ {
		lo, hi := 0, i-1
		for lo <= hi {
			mid := (lo + hi) / 2
			if at[i].quality < at[mid].quality ||
				(at[i].quality == at[mid].quality && at[i].specificity < at[mid].specificity) ||
				(at[i].quality == at[mid].quality && at[i].specificity < at[mid].specificity && len(at[i].params) < len(at[mid].params)) ||
				(at[i].quality == at[mid].quality && at[i].specificity == at[mid].specificity && len(at[i].params) == len(at[mid].params) && at[i].order > at[mid].order) {
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		for j := i; j > lo; j-- {
			at[j-1], at[j] = at[j], at[j-1]
		}
	}
}

func matchEtag(s, etag string) bool {
	if s == etag || s == "W/"+etag || "W/"+s == etag {
		return true
	}

	return false
}

func (app *App) isEtagStale(etag string, noneMatchBytes []byte) bool {
	var start, end int

	// Adapted from:
	// https://github.com/jshttp/fresh/blob/10e0471669dbbfbfd8de65bc6efac2ddd0bfa057/index.js#L110
	for i := range noneMatchBytes {
		switch noneMatchBytes[i] {
		case 0x20:
			if start == end {
				start = i + 1
				end = i + 1
			}
		case 0x2c:
			if matchEtag(app.getString(noneMatchBytes[start:end]), etag) {
				return false
			}
			start = i + 1
			end = i + 1
		default:
			end = i + 1
		}
	}

	return !matchEtag(app.getString(noneMatchBytes[start:end]), etag)
}

func parseAddr(raw string) (string, string) { //nolint:revive // Returns (host, port)
	if i := strings.LastIndex(raw, ":"); i != -1 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}

const noCacheValue = "no-cache"

// isNoCache checks if the cacheControl header value is a `no-cache`.
func isNoCache(cacheControl string) bool {
	i := strings.Index(cacheControl, noCacheValue)
	if i == -1 {
		return false
	}

	// Xno-cache
	if i > 0 && !(cacheControl[i-1] == ' ' || cacheControl[i-1] == ',') {
		return false
	}

	// bla bla, no-cache
	if i+len(noCacheValue) == len(cacheControl) {
		return true
	}

	// bla bla, no-cacheX
	if cacheControl[i+len(noCacheValue)] != ',' {
		return false
	}

	// OK
	return true
}

type testConn struct {
	r bytes.Buffer
	w bytes.Buffer
}

func (c *testConn) Read(b []byte) (int, error)  { return c.r.Read(b) }  //nolint:wrapcheck // This must not be wrapped
func (c *testConn) Write(b []byte) (int, error) { return c.w.Write(b) } //nolint:wrapcheck // This must not be wrapped
func (*testConn) Close() error                  { return nil }

func (*testConn) LocalAddr() net.Addr                { return &net.TCPAddr{Port: 0, Zone: "", IP: net.IPv4zero} }
func (*testConn) RemoteAddr() net.Addr               { return &net.TCPAddr{Port: 0, Zone: "", IP: net.IPv4zero} }
func (*testConn) SetDeadline(_ time.Time) error      { return nil }
func (*testConn) SetReadDeadline(_ time.Time) error  { return nil }
func (*testConn) SetWriteDeadline(_ time.Time) error { return nil }

func getStringImmutable(b []byte) string {
	return string(b)
}

func getBytesImmutable(s string) []byte {
	return []byte(s)
}

// HTTP methods and their unique INTs
func (app *App) methodInt(s string) int {
	// For better performance
	if len(app.configured.RequestMethods) == 0 {
		// TODO: Use iota instead
		switch s {
		case MethodGet:
			return 0
		case MethodHead:
			return 1
		case MethodPost:
			return 2
		case MethodPut:
			return 3
		case MethodDelete:
			return 4
		case MethodConnect:
			return 5
		case MethodOptions:
			return 6
		case MethodTrace:
			return 7
		case MethodPatch:
			return 8
		default:
			return -1
		}
	}

	// For method customization
	for i, v := range app.config.RequestMethods {
		if s == v {
			return i
		}
	}

	return -1
}

// IsMethodSafe reports whether the HTTP method is considered safe.
// See https://datatracker.ietf.org/doc/html/rfc9110#section-9.2.1
func IsMethodSafe(m string) bool {
	switch m {
	case MethodGet,
		MethodHead,
		MethodOptions,
		MethodTrace:
		return true
	default:
		return false
	}
}

// IsMethodIdempotent reports whether the HTTP method is considered idempotent.
// See https://datatracker.ietf.org/doc/html/rfc9110#section-9.2.2
func IsMethodIdempotent(m string) bool {
	if IsMethodSafe(m) {
		return true
	}

	switch m {
	case MethodPut, MethodDelete:
		return true
	default:
		return false
	}
}

// HTTP methods were copied from net/http.
const (
	MethodGet     = "GET"     // RFC 7231, 4.3.1
	MethodHead    = "HEAD"    // RFC 7231, 4.3.2
	MethodPost    = "POST"    // RFC 7231, 4.3.3
	MethodPut     = "PUT"     // RFC 7231, 4.3.4
	MethodPatch   = "PATCH"   // RFC 5789
	MethodDelete  = "DELETE"  // RFC 7231, 4.3.5
	MethodConnect = "CONNECT" // RFC 7231, 4.3.6
	MethodOptions = "OPTIONS" // RFC 7231, 4.3.7
	MethodTrace   = "TRACE"   // RFC 7231, 4.3.8
	methodUse     = "USE"
)

// MIME types that are commonly used
const (
	MIMETextXML         = "text/xml"
	MIMETextHTML        = "text/html"
	MIMETextPlain       = "text/plain"
	MIMETextJavaScript  = "text/javascript"
	MIMEApplicationXML  = "application/xml"
	MIMEApplicationJSON = "application/json"
	// Deprecated: use MIMETextJavaScript instead
	MIMEApplicationJavaScript = "application/javascript"
	MIMEApplicationForm       = "application/x-www-form-urlencoded"
	MIMEOctetStream           = "application/octet-stream"
	MIMEMultipartForm         = "multipart/form-data"

	MIMETextXMLCharsetUTF8         = "text/xml; charset=utf-8"
	MIMETextHTMLCharsetUTF8        = "text/html; charset=utf-8"
	MIMETextPlainCharsetUTF8       = "text/plain; charset=utf-8"
	MIMETextJavaScriptCharsetUTF8  = "text/javascript; charset=utf-8"
	MIMEApplicationXMLCharsetUTF8  = "application/xml; charset=utf-8"
	MIMEApplicationJSONCharsetUTF8 = "application/json; charset=utf-8"
	// Deprecated: use MIMETextJavaScriptCharsetUTF8 instead
	MIMEApplicationJavaScriptCharsetUTF8 = "application/javascript; charset=utf-8"
)

// HTTP status codes were copied from net/http with the following updates:
// - Rename StatusNonAuthoritativeInfo to StatusNonAuthoritativeInformation
// - Add StatusSwitchProxy (306)
// NOTE: Keep this list in sync with statusMessage
const (
	StatusContinue           = 100 // RFC 9110, 15.2.1
	StatusSwitchingProtocols = 101 // RFC 9110, 15.2.2
	StatusProcessing         = 102 // RFC 2518, 10.1
	StatusEarlyHints         = 103 // RFC 8297

	StatusOK                          = 200 // RFC 9110, 15.3.1
	StatusCreated                     = 201 // RFC 9110, 15.3.2
	StatusAccepted                    = 202 // RFC 9110, 15.3.3
	StatusNonAuthoritativeInformation = 203 // RFC 9110, 15.3.4
	StatusNoContent                   = 204 // RFC 9110, 15.3.5
	StatusResetContent                = 205 // RFC 9110, 15.3.6
	StatusPartialContent              = 206 // RFC 9110, 15.3.7
	StatusMultiStatus                 = 207 // RFC 4918, 11.1
	StatusAlreadyReported             = 208 // RFC 5842, 7.1
	StatusIMUsed                      = 226 // RFC 3229, 10.4.1

	StatusMultipleChoices   = 300 // RFC 9110, 15.4.1
	StatusMovedPermanently  = 301 // RFC 9110, 15.4.2
	StatusFound             = 302 // RFC 9110, 15.4.3
	StatusSeeOther          = 303 // RFC 9110, 15.4.4
	StatusNotModified       = 304 // RFC 9110, 15.4.5
	StatusUseProxy          = 305 // RFC 9110, 15.4.6
	StatusSwitchProxy       = 306 // RFC 9110, 15.4.7 (Unused)
	StatusTemporaryRedirect = 307 // RFC 9110, 15.4.8
	StatusPermanentRedirect = 308 // RFC 9110, 15.4.9

	StatusBadRequest                   = 400 // RFC 9110, 15.5.1
	StatusUnauthorized                 = 401 // RFC 9110, 15.5.2
	StatusPaymentRequired              = 402 // RFC 9110, 15.5.3
	StatusForbidden                    = 403 // RFC 9110, 15.5.4
	StatusNotFound                     = 404 // RFC 9110, 15.5.5
	StatusMethodNotAllowed             = 405 // RFC 9110, 15.5.6
	StatusNotAcceptable                = 406 // RFC 9110, 15.5.7
	StatusProxyAuthRequired            = 407 // RFC 9110, 15.5.8
	StatusRequestTimeout               = 408 // RFC 9110, 15.5.9
	StatusConflict                     = 409 // RFC 9110, 15.5.10
	StatusGone                         = 410 // RFC 9110, 15.5.11
	StatusLengthRequired               = 411 // RFC 9110, 15.5.12
	StatusPreconditionFailed           = 412 // RFC 9110, 15.5.13
	StatusRequestEntityTooLarge        = 413 // RFC 9110, 15.5.14
	StatusRequestURITooLong            = 414 // RFC 9110, 15.5.15
	StatusUnsupportedMediaType         = 415 // RFC 9110, 15.5.16
	StatusRequestedRangeNotSatisfiable = 416 // RFC 9110, 15.5.17
	StatusExpectationFailed            = 417 // RFC 9110, 15.5.18
	StatusTeapot                       = 418 // RFC 9110, 15.5.19 (Unused)
	StatusMisdirectedRequest           = 421 // RFC 9110, 15.5.20
	StatusUnprocessableEntity          = 422 // RFC 9110, 15.5.21
	StatusLocked                       = 423 // RFC 4918, 11.3
	StatusFailedDependency             = 424 // RFC 4918, 11.4
	StatusTooEarly                     = 425 // RFC 8470, 5.2.
	StatusUpgradeRequired              = 426 // RFC 9110, 15.5.22
	StatusPreconditionRequired         = 428 // RFC 6585, 3
	StatusTooManyRequests              = 429 // RFC 6585, 4
	StatusRequestHeaderFieldsTooLarge  = 431 // RFC 6585, 5
	StatusUnavailableForLegalReasons   = 451 // RFC 7725, 3

	StatusInternalServerError           = 500 // RFC 9110, 15.6.1
	StatusNotImplemented                = 501 // RFC 9110, 15.6.2
	StatusBadGateway                    = 502 // RFC 9110, 15.6.3
	StatusServiceUnavailable            = 503 // RFC 9110, 15.6.4
	StatusGatewayTimeout                = 504 // RFC 9110, 15.6.5
	StatusHTTPVersionNotSupported       = 505 // RFC 9110, 15.6.6
	StatusVariantAlsoNegotiates         = 506 // RFC 2295, 8.1
	StatusInsufficientStorage           = 507 // RFC 4918, 11.5
	StatusLoopDetected                  = 508 // RFC 5842, 7.2
	StatusNotExtended                   = 510 // RFC 2774, 7
	StatusNetworkAuthenticationRequired = 511 // RFC 6585, 6
)

// Errors
var (
	ErrBadRequest                   = NewError(StatusBadRequest)                   // 400
	ErrUnauthorized                 = NewError(StatusUnauthorized)                 // 401
	ErrPaymentRequired              = NewError(StatusPaymentRequired)              // 402
	ErrForbidden                    = NewError(StatusForbidden)                    // 403
	ErrNotFound                     = NewError(StatusNotFound)                     // 404
	ErrMethodNotAllowed             = NewError(StatusMethodNotAllowed)             // 405
	ErrNotAcceptable                = NewError(StatusNotAcceptable)                // 406
	ErrProxyAuthRequired            = NewError(StatusProxyAuthRequired)            // 407
	ErrRequestTimeout               = NewError(StatusRequestTimeout)               // 408
	ErrConflict                     = NewError(StatusConflict)                     // 409
	ErrGone                         = NewError(StatusGone)                         // 410
	ErrLengthRequired               = NewError(StatusLengthRequired)               // 411
	ErrPreconditionFailed           = NewError(StatusPreconditionFailed)           // 412
	ErrRequestEntityTooLarge        = NewError(StatusRequestEntityTooLarge)        // 413
	ErrRequestURITooLong            = NewError(StatusRequestURITooLong)            // 414
	ErrUnsupportedMediaType         = NewError(StatusUnsupportedMediaType)         // 415
	ErrRequestedRangeNotSatisfiable = NewError(StatusRequestedRangeNotSatisfiable) // 416
	ErrExpectationFailed            = NewError(StatusExpectationFailed)            // 417
	ErrTeapot                       = NewError(StatusTeapot)                       // 418
	ErrMisdirectedRequest           = NewError(StatusMisdirectedRequest)           // 421
	ErrUnprocessableEntity          = NewError(StatusUnprocessableEntity)          // 422
	ErrLocked                       = NewError(StatusLocked)                       // 423
	ErrFailedDependency             = NewError(StatusFailedDependency)             // 424
	ErrTooEarly                     = NewError(StatusTooEarly)                     // 425
	ErrUpgradeRequired              = NewError(StatusUpgradeRequired)              // 426
	ErrPreconditionRequired         = NewError(StatusPreconditionRequired)         // 428
	ErrTooManyRequests              = NewError(StatusTooManyRequests)              // 429
	ErrRequestHeaderFieldsTooLarge  = NewError(StatusRequestHeaderFieldsTooLarge)  // 431
	ErrUnavailableForLegalReasons   = NewError(StatusUnavailableForLegalReasons)   // 451

	ErrInternalServerError           = NewError(StatusInternalServerError)           // 500
	ErrNotImplemented                = NewError(StatusNotImplemented)                // 501
	ErrBadGateway                    = NewError(StatusBadGateway)                    // 502
	ErrServiceUnavailable            = NewError(StatusServiceUnavailable)            // 503
	ErrGatewayTimeout                = NewError(StatusGatewayTimeout)                // 504
	ErrHTTPVersionNotSupported       = NewError(StatusHTTPVersionNotSupported)       // 505
	ErrVariantAlsoNegotiates         = NewError(StatusVariantAlsoNegotiates)         // 506
	ErrInsufficientStorage           = NewError(StatusInsufficientStorage)           // 507
	ErrLoopDetected                  = NewError(StatusLoopDetected)                  // 508
	ErrNotExtended                   = NewError(StatusNotExtended)                   // 510
	ErrNetworkAuthenticationRequired = NewError(StatusNetworkAuthenticationRequired) // 511
)

// HTTP Headers were copied from net/http.
const (
	HeaderAuthorization                      = "Authorization"
	HeaderProxyAuthenticate                  = "Proxy-Authenticate"
	HeaderProxyAuthorization                 = "Proxy-Authorization"
	HeaderWWWAuthenticate                    = "WWW-Authenticate"
	HeaderAge                                = "Age"
	HeaderCacheControl                       = "Cache-Control"
	HeaderClearSiteData                      = "Clear-Site-Data"
	HeaderExpires                            = "Expires"
	HeaderPragma                             = "Pragma"
	HeaderWarning                            = "Warning"
	HeaderAcceptCH                           = "Accept-CH"
	HeaderAcceptCHLifetime                   = "Accept-CH-Lifetime"
	HeaderContentDPR                         = "Content-DPR"
	HeaderDPR                                = "DPR"
	HeaderEarlyData                          = "Early-Data"
	HeaderSaveData                           = "Save-Data"
	HeaderViewportWidth                      = "Viewport-Width"
	HeaderWidth                              = "Width"
	HeaderETag                               = "ETag"
	HeaderIfMatch                            = "If-Match"
	HeaderIfModifiedSince                    = "If-Modified-Since"
	HeaderIfNoneMatch                        = "If-None-Match"
	HeaderIfUnmodifiedSince                  = "If-Unmodified-Since"
	HeaderLastModified                       = "Last-Modified"
	HeaderVary                               = "Vary"
	HeaderConnection                         = "Connection"
	HeaderKeepAlive                          = "Keep-Alive"
	HeaderAccept                             = "Accept"
	HeaderAcceptCharset                      = "Accept-Charset"
	HeaderAcceptEncoding                     = "Accept-Encoding"
	HeaderAcceptLanguage                     = "Accept-Language"
	HeaderCookie                             = "Cookie"
	HeaderExpect                             = "Expect"
	HeaderMaxForwards                        = "Max-Forwards"
	HeaderSetCookie                          = "Set-Cookie"
	HeaderAccessControlAllowCredentials      = "Access-Control-Allow-Credentials"
	HeaderAccessControlAllowHeaders          = "Access-Control-Allow-Headers"
	HeaderAccessControlAllowMethods          = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowOrigin           = "Access-Control-Allow-Origin"
	HeaderAccessControlExposeHeaders         = "Access-Control-Expose-Headers"
	HeaderAccessControlMaxAge                = "Access-Control-Max-Age"
	HeaderAccessControlRequestHeaders        = "Access-Control-Request-Headers"
	HeaderAccessControlRequestMethod         = "Access-Control-Request-Method"
	HeaderOrigin                             = "Origin"
	HeaderTimingAllowOrigin                  = "Timing-Allow-Origin"
	HeaderXPermittedCrossDomainPolicies      = "X-Permitted-Cross-Domain-Policies"
	HeaderDNT                                = "DNT"
	HeaderTk                                 = "Tk"
	HeaderContentDisposition                 = "Content-Disposition"
	HeaderContentEncoding                    = "Content-Encoding"
	HeaderContentLanguage                    = "Content-Language"
	HeaderContentLength                      = "Content-Length"
	HeaderContentLocation                    = "Content-Location"
	HeaderContentType                        = "Content-Type"
	HeaderForwarded                          = "Forwarded"
	HeaderVia                                = "Via"
	HeaderXForwardedFor                      = "X-Forwarded-For"
	HeaderXForwardedHost                     = "X-Forwarded-Host"
	HeaderXForwardedProto                    = "X-Forwarded-Proto"
	HeaderXForwardedProtocol                 = "X-Forwarded-Protocol"
	HeaderXForwardedSsl                      = "X-Forwarded-Ssl"
	HeaderXUrlScheme                         = "X-Url-Scheme"
	HeaderLocation                           = "Location"
	HeaderFrom                               = "From"
	HeaderHost                               = "Host"
	HeaderReferer                            = "Referer"
	HeaderReferrerPolicy                     = "Referrer-Policy"
	HeaderUserAgent                          = "User-Agent"
	HeaderAllow                              = "Allow"
	HeaderServer                             = "Server"
	HeaderAcceptRanges                       = "Accept-Ranges"
	HeaderContentRange                       = "Content-Range"
	HeaderIfRange                            = "If-Range"
	HeaderRange                              = "Range"
	HeaderContentSecurityPolicy              = "Content-Security-Policy"
	HeaderContentSecurityPolicyReportOnly    = "Content-Security-Policy-Report-Only"
	HeaderCrossOriginResourcePolicy          = "Cross-Origin-Resource-Policy"
	HeaderExpectCT                           = "Expect-CT"
	HeaderPermissionsPolicy                  = "Permissions-Policy"
	HeaderPublicKeyPins                      = "Public-Key-Pins"
	HeaderPublicKeyPinsReportOnly            = "Public-Key-Pins-Report-Only"
	HeaderStrictTransportSecurity            = "Strict-Transport-Security"
	HeaderUpgradeInsecureRequests            = "Upgrade-Insecure-Requests"
	HeaderXContentTypeOptions                = "X-Content-Type-Options"
	HeaderXDownloadOptions                   = "X-Download-Options"
	HeaderXFrameOptions                      = "X-Frame-Options"
	HeaderXPoweredBy                         = "X-Powered-By"
	HeaderXXSSProtection                     = "X-XSS-Protection"
	HeaderLastEventID                        = "Last-Event-ID"
	HeaderNEL                                = "NEL"
	HeaderPingFrom                           = "Ping-From"
	HeaderPingTo                             = "Ping-To"
	HeaderReportTo                           = "Report-To"
	HeaderTE                                 = "TE"
	HeaderTrailer                            = "Trailer"
	HeaderTransferEncoding                   = "Transfer-Encoding"
	HeaderSecWebSocketAccept                 = "Sec-WebSocket-Accept"
	HeaderSecWebSocketExtensions             = "Sec-WebSocket-Extensions"
	HeaderSecWebSocketKey                    = "Sec-WebSocket-Key"
	HeaderSecWebSocketProtocol               = "Sec-WebSocket-Protocol"
	HeaderSecWebSocketVersion                = "Sec-WebSocket-Version"
	HeaderAcceptPatch                        = "Accept-Patch"
	HeaderAcceptPushPolicy                   = "Accept-Push-Policy"
	HeaderAcceptSignature                    = "Accept-Signature"
	HeaderAltSvc                             = "Alt-Svc"
	HeaderDate                               = "Date"
	HeaderIndex                              = "Index"
	HeaderLargeAllocation                    = "Large-Allocation"
	HeaderLink                               = "Link"
	HeaderPushPolicy                         = "Push-Policy"
	HeaderRetryAfter                         = "Retry-After"
	HeaderServerTiming                       = "Server-Timing"
	HeaderSignature                          = "Signature"
	HeaderSignedHeaders                      = "Signed-Headers"
	HeaderSourceMap                          = "SourceMap"
	HeaderUpgrade                            = "Upgrade"
	HeaderXDNSPrefetchControl                = "X-DNS-Prefetch-Control"
	HeaderXPingback                          = "X-Pingback"
	HeaderXRequestID                         = "X-Request-ID"
	HeaderXRequestedWith                     = "X-Requested-With"
	HeaderXRobotsTag                         = "X-Robots-Tag"
	HeaderXUACompatible                      = "X-UA-Compatible"
	HeaderAccessControlAllowPrivateNetwork   = "Access-Control-Allow-Private-Network"
	HeaderAccessControlRequestPrivateNetwork = "Access-Control-Request-Private-Network"
)

// Network types that are commonly used
const (
	NetworkTCP  = "tcp"
	NetworkTCP4 = "tcp4"
	NetworkTCP6 = "tcp6"
)

// Compression types
const (
	StrGzip    = "gzip"
	StrBr      = "br"
	StrDeflate = "deflate"
	StrBrotli  = "brotli"
)

// Cookie SameSite
// https://datatracker.ietf.org/doc/html/draft-ietf-httpbis-rfc6265bis-03#section-4.1.2.7
const (
	CookieSameSiteDisabled   = "disabled" // not in RFC, just control "SameSite" attribute will not be set.
	CookieSameSiteLaxMode    = "lax"
	CookieSameSiteStrictMode = "strict"
	CookieSameSiteNoneMode   = "none"
)

// Route Constraints
const (
	ConstraintInt             = "int"
	ConstraintBool            = "bool"
	ConstraintFloat           = "float"
	ConstraintAlpha           = "alpha"
	ConstraintGUID            = "guid"
	ConstraintMinLen          = "minLen"
	ConstraintMaxLen          = "maxLen"
	ConstraintLen             = "len"
	ConstraintBetweenLen      = "betweenLen"
	ConstraintMinLenLower     = "minlen"
	ConstraintMaxLenLower     = "maxlen"
	ConstraintBetweenLenLower = "betweenlen"
	ConstraintMin             = "min"
	ConstraintMax             = "max"
	ConstraintRange           = "range"
	ConstraintDatetime        = "datetime"
	ConstraintRegex           = "regex"
)

func IndexRune(str string, needle int32) bool {
	for _, b := range str {
		if b == needle {
			return true
		}
	}
	return false
}
