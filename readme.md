# payload

`payload` is a Go package for decoding HTTP request data into structs using struct tags. It handles headers, cookies, query parameters, URL path parameters, and request bodies (JSON, XML, and form-encoded) — all in a single `Decode` call.

```go
type CreateUserRequest struct {
    // from the URL path: /users/{id}
    ID int `path:"id"`
    // from ?include_deleted=true
    IncludeDeleted bool `query:"include_deleted"`
    // from the Authorization header
    Token string `header:"Authorization"`
    // from the request body (JSON / XML / form)
    Name  string `json:"name"  xml:"name"  form:"name"`
    Email string `json:"email" xml:"email" form:"email"`
}
```

---

## Table of Contents

- [Getting Started](#getting-started)
  - [Requirements](#requirements)
  - [Installation](#installation)
  - [Basic Example](#basic-example)
- [Usage](#usage)
  - [Decoding Individual Sources](#decoding-individual-sources)
  - [Request Body Formats](#request-body-formats)
  - [Custom Unmarshalers](#custom-unmarshalers)
  - [Registering Custom Types](#registering-custom-types)
  - [Registering Custom Content Types](#registering-custom-content-types)
  - [Error Handling](#error-handling)
- [Struct Tag Reference](#struct-tag-reference)

---

## Getting Started

### Requirements

- Go 1.22 or later (path parameter support via `(*http.Request).PathValue` requires Go 1.22+)

### Installation

```bash
go get github.com/canpacis/payload
```

### Basic Example

Define a struct with tags for whichever request sources you care about, then call `payload.UnmarshalRequest` inside your handler. Fields without a matching tag for the current source are silently ignored.

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/canpacis/payload"
)

type ListPostsRequest struct {
    // GET /posts/{category}?page=2&limit=10
    // Cookie: session=abc123
    // Authorization: Bearer <token>
    Category string `path:"category"`
    Page     int    `query:"page"`
    Limit    int    `query:"limit"`
    Session  string `cookie:"session"`
    Token    string `header:"Authorization"`
}

func listPostsHandler(w http.ResponseWriter, r *http.Request) {
    var req ListPostsRequest
    if err := payload.UnmarshalRequest(r, &req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    // req.Category  -> path parameter
    // req.Page      -> query parameter (default 0 if absent)
    // req.Session   -> cookie value
    // req.Token     -> Authorization header value

    json.NewEncoder(w).Encode(req)
}

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /posts/{category}", listPostsHandler)
    http.ListenAndServe(":8080", mux)
}
```

---

## Usage

### Decoding Individual Sources

You don't have to decode the whole request at once. Each source has its own standalone function when you only need one:

```go
// Headers only
var h struct {
    Accept  string `header:"Accept"`
    TraceID string `header:"X-Trace-Id"`
}
payload.UnmarshalHeader(r.Header, &h)

// Cookies only
var c struct {
    Session string `cookie:"session"`
    Theme   string `cookie:"theme"`
}
payload.UnmarshalCookie(r, &c)

// Query parameters only
var q struct {
    Page int    `query:"page"`
    Sort string `query:"sort"`
}
payload.UnmarshalQuery(r.URL.Query(), &q)

// URL path parameters only (Go 1.22+ ServeMux)
var p struct {
    UserID int    `path:"user_id"`
    Slug   string `path:"slug"`
}
payload.UnmarshalPath(r, &p)
```

Each function also has a constructor equivalent if you need to reuse the decoder across multiple requests:

```go
decoder := payload.NewHeaderDecoder(r.Header)
decoder.Decode(&h)
```

### Request Body Formats

`UnmarshalRequest` (and `RequestDecoder.Decode`) automatically selects a body decoder based on the `Content-Type` header for `POST`, `PUT`, `PATCH`, and `DELETE` requests. `GET` requests never read the body.

**JSON** — use standard `json` struct tags:

```go
type CreateOrderRequest struct {
    ProductID int    `json:"product_id"`
    Quantity  int    `json:"quantity"`
    Note      string `json:"note"`
}
// Content-Type: application/json
// {"product_id": 5, "quantity": 2, "note": "leave at door"}
```

**XML** — use standard `xml` struct tags:

```go
type UpdateProfileRequest struct {
    DisplayName string `xml:"display_name"`
    Bio         string `xml:"bio"`
}
// Content-Type: application/xml
// <UpdateProfileRequest><display_name>Alice</display_name><bio>...</bio></UpdateProfileRequest>
```

**Form-encoded** — use `form` struct tags:

```go
type LoginRequest struct {
    Username string `form:"username"`
    Password string `form:"password"`
}
// Content-Type: application/x-www-form-urlencoded
// username=alice&password=hunter2
```

You can mix body tags with source tags on the same struct — each decoder only processes the tags it knows about:

```go
type CreateCommentRequest struct {
    PostID    int    `path:"post_id"`
    UserAgent string `header:"User-Agent"`
    Body      string `json:"body"`
    Rating    int    `json:"rating"`
}
```

### Custom Unmarshalers

For types that need bespoke parsing from a specific source, implement the corresponding interface. The decoder calls your method instead of the built-in string conversion.

```go
type Permission uint8

const (
    PermRead  Permission = 1 << iota
    PermWrite
    PermAdmin
)

// UnmarshalQuery is called when this type appears in a `query`-tagged field.
func (p *Permission) UnmarshalQuery(s string) error {
    switch s {
    case "read":
        *p = PermRead
    case "write":
        *p = PermWrite
    case "admin":
        *p = PermAdmin
    default:
        return fmt.Errorf("unknown permission: %q", s)
    }
    return nil
}

type SearchRequest struct {
    // GET /search?perm=write
    Perm Permission `query:"perm"`
}
```

The four interfaces and their corresponding source tags are:

| Interface           | Tag      |
| ------------------- | -------- |
| `HeaderUnmarshaler` | `header` |
| `CookieUnmarshaler` | `cookie` |
| `QueryUnmarshaler`  | `query`  |
| `PathUnmarshaler`   | `path`   |

### Registering Custom Types

`RegisterType` lets you teach the decoder how to parse any concrete type from a string without modifying the type itself — useful for third-party types like `time.Time` or `uuid.UUID`:

```go
import (
    "time"
    "github.com/google/uuid"
    "github.com/canpacis/payload"
)

func init() {
    // Parse RFC3339 timestamps from any string source (query, header, path, cookie)
    payload.RegisterType(time.Time{}, func(s string) (time.Time, error) {
        return time.Parse(time.RFC3339, s)
    })

    // Parse UUIDs
    payload.RegisterType(uuid.UUID{}, func(s string) (uuid.UUID, error) {
        return uuid.Parse(s)
    })
}

type GetEventRequest struct {
    // GET /events/{id}?after=2024-01-01T00:00:00Z
    ID    uuid.UUID `path:"id"`
    After time.Time `query:"after"`
}
```

Registered types take precedence over built-in kind-level parsers (int, float, etc.) but are overridden by a type that implements the source-specific unmarshaler interface.

### Registering Custom Content Types

If your API accepts a body format beyond JSON, XML, and form-encoding, register a `Decoder` for that MIME type. Registrations can be global (applied to all `UnmarshalRequest` calls) or scoped to a single `RequestDecoder` instance.

**Global registration:**

```go
// Implement the payload.Decoder interface for your format.
type msgpackDecoder struct {
    r io.Reader
}

func (d *msgpackDecoder) Decode(v any) error {
    return msgpack.NewDecoder(d.r).Decode(v)
}

func init() {
    // This decoder won't have access to the request body at init time;
    // use per-handler registration (below) when you need the request body.
    payload.RegisterContentType("application/msgpack", &msgpackDecoder{})
}
```

**Per-handler registration** (recommended when the decoder needs the request body):

```go
func uploadHandler(w http.ResponseWriter, r *http.Request) {
    var req MyRequest

    d := payload.NewRequestDecoder(r)
    d.RegisterContentType("application/msgpack", &msgpackDecoder{r: r.Body})

    if err := d.Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
}
```

### Error Handling

`payload` returns typed errors that you can inspect with `errors.As`:

```go
var req MyRequest
if err := payload.UnmarshalRequest(r, &req); err != nil {
    var typeErr     *payload.UnmarshalTypeError
    var marshalErr  *payload.UnmarshalerError
    var invalidErr  *payload.InvalidUnmarshalError

    switch {
    case errors.As(err, &typeErr):
        // A field value from the request could not be converted to the
        // target Go type.
        // typeErr.Field  - struct field name
        // typeErr.Struct - struct type name
        // typeErr.Value  - description of the source value
        // typeErr.Type   - the target reflect.Type
        http.Error(w, fmt.Sprintf("bad value for field %s", typeErr.Field), http.StatusBadRequest)

    case errors.As(err, &marshalErr):
        // A custom unmarshaler method returned an error.
        // marshalErr.Unwrap() returns the original error.
        http.Error(w, marshalErr.Error(), http.StatusBadRequest)

    case errors.As(err, &invalidErr):
        // The value passed to Decode was not a non-nil pointer to a struct.
        // This is always a programming error and should not reach production.
        http.Error(w, "internal error", http.StatusInternalServerError)

    default:
        // Body decode error: malformed JSON/XML, form parse failure, etc.
        http.Error(w, err.Error(), http.StatusBadRequest)
    }
}
```

---

## Struct Tag Reference

| Tag      | Source                                   | Methods with body support |
| -------- | ---------------------------------------- | ------------------------- |
| `header` | `http.Header`                            | all                       |
| `cookie` | request cookies                          | all                       |
| `query`  | URL query string                         | all                       |
| `path`   | URL path parameters (Go 1.22+)           | all                       |
| `form`   | `application/x-www-form-urlencoded` body | POST, PUT, PATCH, DELETE  |
| `json`   | `application/json` body                  | POST, PUT, PATCH, DELETE  |
| `xml`    | `application/xml` body                   | POST, PUT, PATCH, DELETE  |

Built-in string-to-type coercions (no extra code needed):

| Go type                                       | Parsed via                                  |
| --------------------------------------------- | ------------------------------------------- |
| `string`                                      | direct assignment                           |
| `int`, `int8`, `int16`, `int32`, `int64`      | `strconv.ParseInt`                          |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | `strconv.ParseUint`                         |
| `float32`, `float64`                          | `strconv.ParseFloat`                        |
| `bool`                                        | `"true", "false"`or`"1", "0"` introspection |
| any other type                                | `RegisterType`                              |
