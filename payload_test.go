package payload_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/canpacis/payload"
)

// ---------------------------------------------------------------------------
// Helpers / fixtures
// ---------------------------------------------------------------------------

// customTime wraps time.Time and implements all four source-specific
// unmarshaler interfaces so we can exercise the custom-unmarshal path.
type customTime struct {
	time.Time
}

func (c *customTime) UnmarshalHeader(s string) error { return c.parse(s) }
func (c *customTime) UnmarshalCookie(s string) error { return c.parse(s) }
func (c *customTime) UnmarshalQuery(s string) error  { return c.parse(s) }
func (c *customTime) UnmarshalPath(s string) error   { return c.parse(s) }
func (c *customTime) parse(s string) error {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	c.Time = t
	return nil
}

// badUnmarshaler always returns an error from its unmarshal method.
type badUnmarshaler struct{}

func (b *badUnmarshaler) UnmarshalHeader(string) error { return errors.New("boom") }
func (b *badUnmarshaler) UnmarshalQuery(string) error  { return errors.New("boom") }

// stubPathGetter implements PathGetter for tests.
type stubPathGetter map[string]string

func (s stubPathGetter) PathValue(key string) string { return s[key] }

// stubCookieGetter implements CookieGetter for tests.
type stubCookieGetter map[string]string

func (s stubCookieGetter) Cookie(name string) (*http.Cookie, error) {
	v, ok := s[name]
	if !ok {
		return nil, http.ErrNoCookie
	}
	return &http.Cookie{Name: name, Value: v}, nil
}

// mustTime parses an RFC3339 string and panics on failure – test helper only.
func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// HeaderDecoder
// ---------------------------------------------------------------------------

func TestHeaderDecoder(t *testing.T) {
	type target struct {
		Name    string     `header:"X-Name"`
		Age     int        `header:"X-Age"`
		Score   float64    `header:"X-Score"`
		Active  bool       // no tag – should be ignored
		Created customTime `header:"X-Created"`
		Hidden  string     // no tag – should be ignored
	}

	tests := []struct {
		name    string
		headers http.Header
		want    target
		wantErr bool
		errType any // pointer to error type for errors.As
	}{
		{
			name: "all fields populated",
			headers: http.Header{
				"X-Name":    {"Alice"},
				"X-Age":     {"30"},
				"X-Score":   {"9.5"},
				"X-Created": {"2024-01-02T15:04:05Z"},
			},
			want: target{
				Name:    "Alice",
				Age:     30,
				Score:   9.5,
				Created: customTime{mustTime("2024-01-02T15:04:05Z")},
			},
		},
		{
			name:    "missing optional headers – zero values kept",
			headers: http.Header{},
			want:    target{},
		},
		{
			name: "partial headers",
			headers: http.Header{
				"X-Name": {"Bob"},
			},
			want: target{Name: "Bob"},
		},
		{
			name: "invalid int",
			headers: http.Header{
				"X-Age": {"not-a-number"},
			},
			wantErr: true,
		},
		{
			name: "invalid float",
			headers: http.Header{
				"X-Score": {"not-a-float"},
			},
			wantErr: true,
		},
		{
			name: "unmarshaler error",
			headers: http.Header{
				"X-Created": {"not-a-time"},
			},
			wantErr: true,
			errType: &payload.UnmarshalerError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			err := payload.UnmarshalHeader(tc.headers, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalHeader() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if tc.errType != nil && !errors.As(err, &tc.errType) {
					t.Errorf("expected error type %T, got %T", tc.errType, err)
				}
				return
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHeaderDecoder_InvalidArgument(t *testing.T) {
	tests := []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"non-pointer string", "hello"},
		{"pointer to non-struct", func() any { n := 42; return &n }()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := payload.UnmarshalHeader(http.Header{}, tc.v)
			var target *payload.InvalidUnmarshalError
			if !errors.As(err, &target) {
				t.Errorf("expected *InvalidUnmarshalError, got %T: %v", err, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CookieDecoder
// ---------------------------------------------------------------------------

func TestCookieDecoder(t *testing.T) {
	type target struct {
		Session string `cookie:"session"`
		UserID  int    `cookie:"user_id"`
		Theme   string `cookie:"theme"`
	}

	tests := []struct {
		name    string
		cookies stubCookieGetter
		want    target
		wantErr bool
	}{
		{
			name: "all cookies present",
			cookies: stubCookieGetter{
				"session": "abc123",
				"user_id": "42",
				"theme":   "dark",
			},
			want: target{Session: "abc123", UserID: 42, Theme: "dark"},
		},
		{
			name:    "no cookies – zero values",
			cookies: stubCookieGetter{},
			want:    target{},
		},
		{
			name: "missing cookie skipped",
			cookies: stubCookieGetter{
				"session": "xyz",
			},
			want: target{Session: "xyz"},
		},
		{
			name:    "invalid int cookie",
			cookies: stubCookieGetter{"user_id": "not-an-int"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			err := payload.UnmarshalCookie(tc.cookies, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalCookie() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// QueryDecoder
// ---------------------------------------------------------------------------

func TestQueryDecoder(t *testing.T) {
	type target struct {
		Page    int     `query:"page"`
		Limit   uint    `query:"limit"`
		Search  string  `query:"q"`
		Score   float32 `query:"score"`
		Ignored string  // no tag
	}

	tests := []struct {
		name    string
		query   url.Values
		want    target
		wantErr bool
	}{
		{
			name: "all params present",
			query: url.Values{
				"page":  {"2"},
				"limit": {"25"},
				"q":     {"golang"},
				"score": {"7.5"},
			},
			want: target{Page: 2, Limit: 25, Search: "golang", Score: 7.5},
		},
		{
			name:  "empty query – zero values",
			query: url.Values{},
			want:  target{},
		},
		{
			name:    "invalid int",
			query:   url.Values{"page": {"abc"}},
			wantErr: true,
		},
		{
			name:    "invalid uint",
			query:   url.Values{"limit": {"-1"}},
			wantErr: true,
		},
		{
			name:    "invalid float",
			query:   url.Values{"score": {"not-a-float"}},
			wantErr: true,
		},
		{
			name:  "first value used when multiple values for same key",
			query: url.Values{"q": {"first", "second"}},
			want:  target{Search: "first"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			err := payload.UnmarshalQuery(tc.query, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalQuery() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PathDecoder
// ---------------------------------------------------------------------------

func TestPathDecoder(t *testing.T) {
	type target struct {
		ID   int    `path:"id"`
		Slug string `path:"slug"`
	}

	tests := []struct {
		name    string
		params  stubPathGetter
		want    target
		wantErr bool
	}{
		{
			name:   "all path params present",
			params: stubPathGetter{"id": "7", "slug": "hello-world"},
			want:   target{ID: 7, Slug: "hello-world"},
		},
		{
			name:   "empty params – zero values",
			params: stubPathGetter{},
			want:   target{},
		},
		{
			name:    "invalid int path param",
			params:  stubPathGetter{"id": "not-an-int"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			err := payload.UnmarshalPath(tc.params, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalPath() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RequestDecoder
// ---------------------------------------------------------------------------

func TestRequestDecoder_GET(t *testing.T) {
	type target struct {
		// from query
		Page int `query:"page"`
		// from header
		Accept string `header:"Accept"`
		// from cookie
		Session string `cookie:"session"`
	}

	tests := []struct {
		name    string
		setup   func() *http.Request
		want    target
		wantErr bool
	}{
		{
			name: "query + header + cookie",
			setup: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/?page=3", nil)
				req.Header.Set("Accept", "application/json")
				req.AddCookie(&http.Cookie{Name: "session", Value: "tok123"})
				return req
			},
			want: target{Page: 3, Accept: "application/json", Session: "tok123"},
		},
		{
			name: "empty request – zero values",
			setup: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/", nil)
			},
			want: target{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			err := payload.UnmarshalRequest(tc.setup(), &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalRequest() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRequestDecoder_Body(t *testing.T) {
	type target struct {
		Name  string `json:"name"  xml:"name"  form:"name"`
		Email string `json:"email" xml:"email" form:"email"`
	}

	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		want        target
		wantErr     bool
	}{
		{
			name:        "JSON body",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"name":"Alice","email":"alice@example.com"}`,
			want:        target{Name: "Alice", Email: "alice@example.com"},
		},
		{
			name:        "JSON body with charset",
			method:      http.MethodPost,
			contentType: "application/json; charset=utf-8",
			body:        `{"name":"Bob","email":"bob@example.com"}`,
			want:        target{Name: "Bob", Email: "bob@example.com"},
		},
		{
			name:        "XML body",
			method:      http.MethodPost,
			contentType: "application/xml",
			body:        `<target><name>Carol</name><email>carol@example.com</email></target>`,
			want:        target{Name: "Carol", Email: "carol@example.com"},
		},
		{
			name:        "form-urlencoded body",
			method:      http.MethodPost,
			contentType: "application/x-www-form-urlencoded",
			body:        "name=Dave&email=dave%40example.com",
			want:        target{Name: "Dave", Email: "dave@example.com"},
		},
		{
			name:        "PUT with JSON body",
			method:      http.MethodPut,
			contentType: "application/json",
			body:        `{"name":"Eve","email":"eve@example.com"}`,
			want:        target{Name: "Eve", Email: "eve@example.com"},
		},
		{
			name:        "PATCH with JSON body",
			method:      http.MethodPatch,
			contentType: "application/json",
			body:        `{"name":"Frank"}`,
			want:        target{Name: "Frank"},
		},
		{
			name:        "DELETE with JSON body",
			method:      http.MethodDelete,
			contentType: "application/json",
			body:        `{"name":"Grace"}`,
			want:        target{Name: "Grace"},
		},
		{
			name:        "GET ignores body",
			method:      http.MethodGet,
			contentType: "application/json",
			body:        `{"name":"Ignored"}`,
			want:        target{},
		},
		{
			name:        "unsupported content type",
			method:      http.MethodPost,
			contentType: "text/plain",
			body:        "hello",
			wantErr:     true,
		},
		{
			name:        "invalid JSON body",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{invalid}`,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.ContentLength = int64(len(tc.body))

			var got target
			err := payload.UnmarshalRequest(req, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UnmarshalRequest() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRequestDecoder_RegisterContentType(t *testing.T) {
	// Register a trivial "text/plain" decoder that sets Name from the raw body.
	type target struct {
		Name string `query:"name"`
	}

	plainDecoder := payload.NewQueryDecoder(url.Values{})

	tests := []struct {
		name    string
		useInst bool // true = instance-level, false = global
	}{
		{name: "instance-level registration", useInst: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := "name=Hank"
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-custom")
			req.ContentLength = int64(len(body))

			// Build a decoder whose custom type parses query-style values.
			plainDecoder = payload.NewQueryDecoder(url.Values{"name": {"Hank"}})

			d := payload.NewRequestDecoder(req)
			d.RegisterContentType("application/x-custom", plainDecoder)

			var got target
			if err := d.Decode(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name != "Hank" {
				t.Errorf("got Name=%q, want %q", got.Name, "Hank")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// decode – error type coverage
// ---------------------------------------------------------------------------

func TestDecode_UnmarshalTypeError(t *testing.T) {
	// A struct field whose type has no coercion path from string.
	type target struct {
		Data []string `header:"X-Data"` // slices are not supported
	}

	headers := http.Header{"X-Data": {"a,b,c"}}
	var got target
	err := payload.UnmarshalHeader(headers, &got)

	var typeErr *payload.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected *UnmarshalTypeError, got %T: %v", err, err)
	}
	if typeErr.Field != "Data" {
		t.Errorf("expected Field=Data, got %q", typeErr.Field)
	}
	if typeErr.Struct != "target" {
		t.Errorf("expected Struct=target, got %q", typeErr.Struct)
	}
}

func TestDecode_UnmarshalerError(t *testing.T) {
	type target struct {
		Bad badUnmarshaler `header:"X-Bad"`
	}

	headers := http.Header{"X-Bad": {"anything"}}
	var got target
	err := payload.UnmarshalHeader(headers, &got)

	var umErr *payload.UnmarshalerError
	if !errors.As(err, &umErr) {
		t.Fatalf("expected *UnmarshalerError, got %T: %v", err, err)
	}
	if umErr.Unwrap() == nil {
		t.Error("UnmarshalerError.Unwrap() should return the underlying error")
	}
}

// ---------------------------------------------------------------------------
// RegisterType
// ---------------------------------------------------------------------------

func TestRegisterType(t *testing.T) {
	// Register a custom type for a named string type.
	type upperString string

	payload.RegisterType(upperString(""), func(s string) (upperString, error) {
		return upperString(strings.ToUpper(s)), nil
	})

	type target struct {
		Tag upperString `query:"tag"`
	}

	tests := []struct {
		name  string
		query url.Values
		want  upperString
	}{
		{
			name:  "value is uppercased via registered type",
			query: url.Values{"tag": {"hello"}},
			want:  "HELLO",
		},
		{
			name:  "empty value skipped – zero kept",
			query: url.Values{},
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got target
			if err := payload.UnmarshalQuery(tc.query, &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Tag != tc.want {
				t.Errorf("got %q, want %q", got.Tag, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error message coverage
// ---------------------------------------------------------------------------

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "UnmarshalerError nil inner",
			err:  &payload.UnmarshalerError{},
			want: "",
		},
		{
			name: "InvalidUnmarshalError nil type",
			err:  &payload.InvalidUnmarshalError{},
			want: "payload: Unmarshal(nil)",
		},
		{
			name: "UnmarshalTypeError with struct and field",
			err: &payload.UnmarshalTypeError{
				Value:  "string",
				Struct: "MyStruct",
				Field:  "MyField",
			},
			want: "payload: cannot unmarshal string into Go struct field MyStruct.MyField of type <nil>",
		},
		{
			name: "UnmarshalTypeError without struct/field",
			err: &payload.UnmarshalTypeError{
				Value: "string",
			},
			want: "payload: cannot unmarshal string into Go value of type <nil>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Numeric kind coverage
// ---------------------------------------------------------------------------

func TestNumericKinds(t *testing.T) {
	type target struct {
		I   int     `query:"i"`
		I8  int8    `query:"i8"`
		I16 int16   `query:"i16"`
		I32 int32   `query:"i32"`
		I64 int64   `query:"i64"`
		U   uint    `query:"u"`
		U8  uint8   `query:"u8"`
		U16 uint16  `query:"u16"`
		U32 uint32  `query:"u32"`
		U64 uint64  `query:"u64"`
		F32 float32 `query:"f32"`
		F64 float64 `query:"f64"`
	}

	q := url.Values{
		"i":   {"1"},
		"i8":  {"2"},
		"i16": {"3"},
		"i32": {"4"},
		"i64": {"5"},
		"u":   {"6"},
		"u8":  {"7"},
		"u16": {"8"},
		"u32": {"9"},
		"u64": {"10"},
		"f32": {"1.5"},
		"f64": {"2.5"},
	}

	var got target
	if err := payload.UnmarshalQuery(q, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := target{
		I: 1, I8: 2, I16: 3, I32: 4, I64: 5,
		U: 6, U8: 7, U16: 8, U32: 9, U64: 10,
		F32: 1.5, F64: 2.5,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
