// Package payload provides utilities for decoding HTTP request data into Go
// structs. It supports extracting values from headers, cookies, query
// parameters, path parameters, and request bodies (JSON, XML, and
// URL-encoded forms). Each source is handled by a dedicated decoder, and
// all sources can be decoded together in a single pass via [RequestDecoder].
//
// Struct fields are mapped to their respective HTTP source using struct tags
// (e.g. `header:"X-My-Header"`, `cookie:"session"`, `query:"page"`). Types
// that need custom parsing logic can implement the corresponding unmarshaler
// interface for their source (e.g. [HeaderUnmarshaler], [CookieUnmarshaler]).
//
// Custom body content types beyond JSON, XML, and form encoding can be
// registered globally via [RegisterContentType] or per-decoder via
// [RequestDecoder.RegisterContentType].
package payload

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Decoder is the common interface implemented by all source-specific decoders
// in this package. Decode populates v with values extracted from the decoder's
// underlying source. v should be a pointer to a struct whose fields are
// annotated with the appropriate struct tags for the source being decoded.
type Decoder interface {
	Decode(any) error
}

// HeaderDecoder decodes HTTP request headers into a struct. Struct fields are
// mapped to header names via `header` struct tags.
type HeaderDecoder struct {
	header http.Header
}

// NewHeaderDecoder returns a [HeaderDecoder] that reads from the provided
// [http.Header].
func NewHeaderDecoder(h http.Header) *HeaderDecoder {
	return &HeaderDecoder{header: h}
}

// HeaderUnmarshaler can be implemented by types that require custom logic when
// being decoded from an HTTP header value. UnmarshalHeader receives the raw
// header string and is responsible for parsing it into the receiver.
type HeaderUnmarshaler interface {
	UnmarshalHeader(string) error
}

// Decode populates v with values from the HTTP headers. Fields on v must be
// tagged with `header:"<header-name>"` to be mapped. If a field's type
// implements [HeaderUnmarshaler], that method is called instead of the default
// string conversion.
func (d *HeaderDecoder) Decode(v any) error {
	return decode(v, "header", func(key string) any {
		return d.header.Get(key)
	}, func(u HeaderUnmarshaler, s string) error {
		return u.UnmarshalHeader(s)
	})
}

// UnmarshalHeader is a convenience function that decodes the provided
// [http.Header] into v. It is equivalent to calling
// [NewHeaderDecoder](h).Decode(v).
func UnmarshalHeader(h http.Header, v any) error {
	return NewHeaderDecoder(h).Decode(v)
}

// CookieGetter is an interface for retrieving a named cookie. It is satisfied
// by [*http.Request].
type CookieGetter interface {
	Cookie(string) (*http.Cookie, error)
}

// CookieDecoder decodes HTTP cookies into a struct. Struct fields are mapped
// to cookie names via `cookie` struct tags.
type CookieDecoder struct {
	getter CookieGetter
}

// NewCookieDecoder returns a [CookieDecoder] that reads cookies via the
// provided [CookieGetter]. An [*http.Request] satisfies this interface
// directly.
func NewCookieDecoder(getter CookieGetter) *CookieDecoder {
	return &CookieDecoder{getter: getter}
}

// CookieUnmarshaler can be implemented by types that require custom logic when
// being decoded from a cookie value. UnmarshalCookie receives the raw cookie
// string and is responsible for parsing it into the receiver.
type CookieUnmarshaler interface {
	UnmarshalCookie(string) error
}

// Decode populates v with values from cookies. Fields on v must be tagged with
// `cookie:"<cookie-name>"` to be mapped. Missing cookies are silently skipped.
// If a field's type implements [CookieUnmarshaler], that method is called
// instead of the default string conversion.
func (d *CookieDecoder) Decode(v any) error {
	return decode(v, "cookie", func(key string) any {
		cookie, err := d.getter.Cookie(key)
		if err != nil {
			return nil
		}
		return cookie.Value
	}, func(u CookieUnmarshaler, s string) error {
		return u.UnmarshalCookie(s)
	})
}

// UnmarshalCookie is a convenience function that decodes cookies from getter
// into v. It is equivalent to calling [NewCookieDecoder](getter).Decode(v).
func UnmarshalCookie(getter CookieGetter, v any) error {
	return NewCookieDecoder(getter).Decode(v)
}

// QueryDecoder decodes URL query parameters (or form values) into a struct.
// Struct fields are mapped to parameter names via the struct tag key stored
// in the decoder (default: "query"; "form" when decoding POST form values).
type QueryDecoder struct {
	// key is the struct tag name used to identify fields for this decoder
	// (e.g. "query" or "form").
	key   string
	query url.Values
}

// NewQueryDecoder returns a [QueryDecoder] that reads from the provided
// [url.Values]. The struct tag key defaults to "query".
func NewQueryDecoder(q url.Values) *QueryDecoder {
	return &QueryDecoder{key: "query", query: q}
}

// QueryUnmarshaler can be implemented by types that require custom logic when
// being decoded from a URL query parameter value. UnmarshalQuery receives the
// raw parameter string and is responsible for parsing it into the receiver.
type QueryUnmarshaler interface {
	UnmarshalQuery(string) error
}

// Decode populates v with values from the URL query parameters. Fields on v
// must be tagged with `query:"<param-name>"` (or `form:"<param-name>"` when
// used for form decoding) to be mapped. If a field's type implements
// [QueryUnmarshaler], that method is called instead of the default string
// conversion.
func (d *QueryDecoder) Decode(v any) error {
	return decode(v, d.key, func(key string) any {
		return d.query.Get(key)
	}, func(u QueryUnmarshaler, s string) error {
		return u.UnmarshalQuery(s)
	})
}

// UnmarshalQuery is a convenience function that decodes the provided
// [url.Values] into v. It is equivalent to calling
// [NewQueryDecoder](q).Decode(v).
func UnmarshalQuery(q url.Values, v any) error {
	return NewQueryDecoder(q).Decode(v)
}

// PathGetter is an interface for retrieving named URL path parameters. It is
// satisfied by [*http.Request] (via [http.Request.PathValue], available in
// Go 1.22+).
type PathGetter interface {
	PathValue(string) string
}

// PathDecoder decodes URL path parameters into a struct. Struct fields are
// mapped to path parameter names via `path` struct tags.
type PathDecoder struct {
	getter PathGetter
}

// NewPathDecoder returns a [PathDecoder] that reads path values via the
// provided [PathGetter]. An [*http.Request] satisfies this interface directly.
func NewPathDecoder(getter PathGetter) *PathDecoder {
	return &PathDecoder{getter: getter}
}

// PathUnmarshaler can be implemented by types that require custom logic when
// being decoded from a URL path parameter value. UnmarshalPath receives the
// raw parameter string and is responsible for parsing it into the receiver.
type PathUnmarshaler interface {
	UnmarshalPath(string) error
}

// Decode populates v with values from the URL path parameters. Fields on v
// must be tagged with `path:"<param-name>"` to be mapped. If a field's type
// implements [PathUnmarshaler], that method is called instead of the default
// string conversion.
func (d *PathDecoder) Decode(v any) error {
	return decode(v, "path", func(key string) any {
		return d.getter.PathValue(key)
	}, func(u PathUnmarshaler, s string) error {
		return u.UnmarshalPath(s)
	})
}

// UnmarshalPath is a convenience function that decodes path parameters from
// getter into v. It is equivalent to calling
// [NewPathDecoder](getter).Decode(v).
func UnmarshalPath(getter PathGetter, v any) error {
	return NewPathDecoder(getter).Decode(v)
}

// RequestDecoder decodes an entire [*http.Request] into a struct in a single
// pass. It extracts values from headers, cookies, query parameters, and path
// parameters unconditionally, and additionally decodes the request body for
// POST, PUT, PATCH, and DELETE requests with a non-empty body.
//
// Built-in body content types:
//   - application/json        — decoded via [encoding/json]
//   - application/xml         — decoded via [encoding/xml]
//   - application/x-www-form-urlencoded — decoded via [QueryDecoder] with `form` tags
//
// Additional content types can be registered via [RegisterContentType] or
// [RequestDecoder.RegisterContentType].
type RequestDecoder struct {
	req          *http.Request
	contentTypes map[string]Decoder
}

// NewRequestDecoder returns a [RequestDecoder] for the given request with an
// empty content-type registry. Use [RegisterContentType] to register custom
// body decoders globally, or [RequestDecoder.RegisterContentType] to register
// them on this instance only.
func NewRequestDecoder(req *http.Request) *RequestDecoder {
	return &RequestDecoder{req: req, contentTypes: map[string]Decoder{}}
}

var ErrUnsupportedContentType = errors.New("unsupported content type")

// Decode populates v from the request by running each applicable decoder in
// sequence: headers, cookies, query params, path params, and (for methods with
// a body) the request body. Decoders are applied in that order, so later
// sources can overwrite values set by earlier ones.
//
// Returns an error if body parsing fails or if the request's Content-Type is
// not recognised and has no registered decoder.
func (d *RequestDecoder) Decode(v any) error {
	decoders := []Decoder{
		NewHeaderDecoder(d.req.Header),
		NewCookieDecoder(d.req),
		NewQueryDecoder(d.req.URL.Query()),
		NewPathDecoder(d.req),
	}

	bodyMethods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	if slices.Contains(bodyMethods, d.req.Method) && d.req.ContentLength != 0 {
		contentType := d.req.Header.Get("Content-Type")
		switch true {
		case strings.Contains(contentType, "application/json"):
			decoders = append(decoders, json.NewDecoder(d.req.Body))
		case strings.Contains(contentType, "application/xml"):
			decoders = append(decoders, xml.NewDecoder(d.req.Body))
		case contentType == "application/x-www-form-urlencoded", contentType == "application/www-form-urlencoded":
			if err := d.req.ParseForm(); err != nil {
				return err
			}
			decoder := NewQueryDecoder(d.req.PostForm)
			decoder.key = "form"
			decoders = append(decoders, decoder)
		// TODO: handle multipart formdata
		// case strings.Contains(contentType, "multipart/form-data"):
		default:
			decoder, ok := d.contentTypes[contentType]
			if !ok {
				return fmt.Errorf("payload: %w \"%s\"", ErrUnsupportedContentType, contentType)
			}
			decoders = append(decoders, decoder)
		}
	}

	for _, decoder := range decoders {
		if err := decoder.Decode(v); err != nil {
			return err
		}
	}
	return nil
}

// RegisterContentType associates a [Decoder] with a MIME type on this
// specific [RequestDecoder] instance. When the request's Content-Type matches
// typ, decoder will be used to decode the body. This takes precedence over
// the built-in decoders only for types not already handled natively (JSON,
// XML, form-encoded).
func (d *RequestDecoder) RegisterContentType(typ string, decoder Decoder) {
	d.contentTypes[typ] = decoder
}

// defaultContentTypes holds globally registered content-type decoders used by
// [UnmarshalRequest].
var defaultContentTypes = map[string]Decoder{}

// RegisterContentType registers a [Decoder] for the given MIME type in the
// global content-type registry. Decoders registered here are used by
// [UnmarshalRequest] for any request whose Content-Type matches typ.
// This is safe to call at program initialisation (e.g. from an init function).
func RegisterContentType(typ string, decoder Decoder) {
	defaultContentTypes[typ] = decoder
}

// UnmarshalRequest is a convenience function that decodes the entire request
// into v using a [RequestDecoder] initialised with the global content-type
// registry. It is equivalent to:
//
//	decoder := NewRequestDecoder(req)
//	decoder.contentTypes = defaultContentTypes
//	decoder.Decode(v)
func UnmarshalRequest(req *http.Request, v any) error {
	decoder := NewRequestDecoder(req)
	decoder.contentTypes = defaultContentTypes
	return decoder.Decode(v)
}
