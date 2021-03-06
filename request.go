package res

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	nats "github.com/nats-io/go-nats"
)

// Request types
const (
	RequestTypeAccess = "access"
	RequestTypeGet    = "get"
	RequestTypeCall   = "call"
	RequestTypeAuth   = "auth"
)

// Request represent a RES request
type Request struct {
	resource
	rtype   string
	method  string
	msg     *nats.Msg
	replied bool // Flag telling if a reply has been made

	// Fields from the request data
	cid        string
	params     json.RawMessage
	token      json.RawMessage
	header     map[string][]string
	host       string
	remoteAddr string
	uri        string
}

// AccessRequest has methods for responding to access requests.
type AccessRequest interface {
	Resource
	Access(get bool, call string)
	AccessDenied()
	AccessGranted()
	NotFound()
	Error(err *Error)
	RawToken() json.RawMessage
	ParseToken(interface{})
	Timeout(d time.Duration)
}

// ModelRequest has methods for responding to model get requests.
type ModelRequest interface {
	Resource
	Model(model interface{})
	QueryModel(model interface{}, query string)
	NotFound()
	Error(err *Error)
	Timeout(d time.Duration)
}

// CollectionRequest has methods for responding to collection get requests.
type CollectionRequest interface {
	Resource
	Collection(collection interface{})
	QueryCollection(collection interface{}, query string)
	NotFound()
	Error(err *Error)
	Timeout(d time.Duration)
}

// CallRequest has methods for responding to call requests.
type CallRequest interface {
	Resource
	Method() string
	CID() string
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
	OK(result interface{})
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	Timeout(d time.Duration)
}

// NewRequest has methods for responding to new call requests.
type NewRequest interface {
	Resource
	CID() string
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
	New(rid Ref)
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	Timeout(d time.Duration)
}

// AuthRequest has methods for responding to auth requests.
type AuthRequest interface {
	Resource
	Method() string
	CID() string
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
	Header() map[string][]string
	Host() string
	RemoteAddr() string
	URI() string
	OK(result interface{})
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	Timeout(d time.Duration)
	TokenEvent(t interface{})
}

// Static responses and events
var (
	responseAccessDenied    = []byte(`{"error":{"code":"system.accessDenied","message":"Access denied"}}`)
	responseInternalError   = []byte(`{"error":{"code":"system.internalError","message":"Internal error"}}`)
	responseNotFound        = []byte(`{"error":{"code":"system.notFound","message":"Not found"}}`)
	responseMethodNotFound  = []byte(`{"error":{"code":"system.methodNotFound","message":"Method not found"}}`)
	responseInvalidParams   = []byte(`{"error":{"code":"system.invalidParams","message":"Invalid parameters"}}`)
	responseMissingResponse = []byte(`{"error":{"code":"system.internalError","message":"Internal error: missing response"}}`)
	responseAccessGranted   = []byte(`{"result":{"get":true,"call":"*"}}`)
)

// Predefined handlers
var (
	// Access handler that provides full get and call access.
	AccessGranted AccessHandler = func(r AccessRequest) {
		r.AccessGranted()
	}

	// Access handler that sends a system.accessDenied error response.
	AccessDenied AccessHandler = func(r AccessRequest) {
		r.AccessDenied()
	}
)

// Type returns the request type. May be "access", "get", "call", or "auth".
func (r *Request) Type() string {
	return r.rtype
}

// Method returns the resource method.
// Empty string for access and get requests.
func (r *Request) Method() string {
	return r.method
}

// CID returns the connection ID of the requesting client connection.
// Empty string for get requests.
func (r *Request) CID() string {
	return r.cid
}

// RawParams returns the JSON encoded method parameters, or nil if the request had no parameters.
// Always returns nil for access and get requests.
func (r *Request) RawParams() json.RawMessage {
	return r.params
}

// RawToken returns the JSON encoded access token, or nil if the request had no token.
// Always returns nil for get requests.
func (r *Request) RawToken() json.RawMessage {
	return r.token
}

// Header returns the HTTP headers sent by client on connect.
// Only set for auth requests.
func (r *Request) Header() map[string][]string {
	return r.header
}

// Host returns the host on which the URL is sought by the client.
// Per RFC 2616, this is either the value of the "Host" header or the host name
// given in the URL itself.
// Only set for auth requests.
func (r *Request) Host() string {
	return r.host
}

// RemoteAddr returns the network address of the client sent on connect.
// The format is not specified.
// Only set for auth requests.
func (r *Request) RemoteAddr() string {
	return r.remoteAddr
}

// URI returns the unmodified Request-URI of the Request-Line
// (RFC 2616, Section 5.1) as sent by the client on connect.
// Only set for auth requests.
func (r *Request) URI() string {
	return r.uri
}

// OK sends a successful response for the request.
// For get requests, the Model or Collection methods is usually used instead.
// For access requests, the Access or AccessGranted methods is usually used instead.
// The result may be nil.
func (r *Request) OK(result interface{}) {
	r.success(result)
}

// Error sends a custom error response for the request.
func (r *Request) Error(err *Error) {
	r.error(err)
}

// NotFound sends a system.notFound response for the request.
func (r *Request) NotFound() {
	r.reply(responseNotFound)
}

// MethodNotFound sends a system.methodNotFound response for the request.
// Only valid for call and auth requests.
func (r *Request) MethodNotFound() {
	r.reply(responseMethodNotFound)
}

// InvalidParams sends a system.invalidParams response.
// An empty message will default to "Invalid parameters".
// Only valid for call and auth requests.
func (r *Request) InvalidParams(message string) {
	if message == "" {
		r.reply(responseInvalidParams)
	} else {
		r.error(&Error{Code: CodeInvalidParams, Message: message})
	}
}

// Access sends a successful response.
// The get flag tells if the client has access to get (read) the resource.
// The call string is a comma separated list of methods that the client can
// call. Eg. "set,foo,bar". A single asterisk character ("*") means the client
// is allowed to call any method. Empty string means no calls are allowed.
// Only valid for access requests.
func (r *Request) Access(get bool, call string) {
	if !get && call == "" {
		r.reply(responseAccessDenied)
	} else {
		r.success(accessResponse{Get: get, Call: call})
	}
}

// AccessDenied sends a system.accessDenied response.
// Only valid for access requests.
func (r *Request) AccessDenied() {
	r.reply(responseAccessDenied)
}

// AccessGranted a successful response granting full access to the resource.
// Same as calling Access(true, "*");
// Only valid for access requests.
func (r *Request) AccessGranted() {
	r.reply(responseAccessGranted)
}

// Model sends a successful model response for the get request.
// The model must marshal into a JSON object.
// Only valid for get requests for a model resource.
func (r *Request) Model(model interface{}) {
	r.model(model, "")
}

// QueryModel sends a successful query model response for the get request.
// The model must marshal into a JSON object.
// Only valid for get requests for a model query resource.
func (r *Request) QueryModel(model interface{}, query string) {
	r.model(model, query)
}

// model sends a successful model response for the get request.
func (r *Request) model(model interface{}, query string) {
	if query != "" && r.query == "" {
		panic("res: query model response on non-query request")
	}
	// [TODO] Marshal model to a json.RawMessage to see if it is a JSON object
	r.success(modelResponse{Model: model, Query: query})
}

// Collection sends a successful collection response for the get request.
// The collection must marshal into a JSON array.
// Only valid for get requests for a collection resource.
func (r *Request) Collection(collection interface{}) {
	r.collection(collection, "")
}

// QueryCollection sends a successful query collection response for the get request.
// The collection must marshal into a JSON array.
// Only valid for get requests for a collection query resource.
func (r *Request) QueryCollection(collection interface{}, query string) {
	r.collection(collection, query)
}

// collection sends a successful collection response for the get request.
func (r *Request) collection(collection interface{}, query string) {
	if query != "" && r.query == "" {
		panic("res: query collection response on non-query request")
	}
	// [TODO] Marshal collection to a json.RawMessage to see if it is a JSON array
	r.success(collectionResponse{Collection: collection, Query: query})
}

// New sends a successful response for the new call request.
// Panics if rid is invalid.
// Only valid for new call requests.
func (r *Request) New(rid Ref) {
	if !rid.IsValid() {
		panic("res: invalid reference RID: " + rid)
	}
	r.success(rid)
}

// ParseParams unmarshals the JSON encoded parameters and stores the result in p.
// If the request has no parameters, ParseParams does nothing.
// On any error, ParseParams panics with a system.invalidParams *Error.
// Only valid for call and auth requests.
func (r *Request) ParseParams(p interface{}) {
	if len(r.params) == 0 {
		return
	}
	err := json.Unmarshal(r.params, p)
	if err != nil {
		panic(&Error{Code: CodeInvalidParams, Message: err.Error()})
	}
}

// ParseToken unmarshals the JSON encoded token and stores the result in t.
// If the request has no token, ParseToken does nothing.
// On any error, ParseToken panics with a system.internalError *Error.
// Not valid for get requests.
func (r *Request) ParseToken(t interface{}) {
	if len(r.token) == 0 {
		return
	}
	err := json.Unmarshal(r.token, t)
	if err != nil {
		panic(InternalError(err))
	}
}

// Timeout attempts to set the timeout duration of the request.
// The call has no effect if the requester has already timed out the request.
func (r *Request) Timeout(d time.Duration) {
	if d < 0 {
		panic("res: negative timeout duration")
	}
	out := []byte(`timeout:"` + strconv.FormatInt(d.Nanoseconds()/1000000, 10) + `"`)
	r.s.rawEvent(r.msg.Reply, out)
}

// TokenEvent sends a connection token event that sets the requester's connection access token,
// discarding any previously set token.
// A change of token will invalidate any previous access response received using the old token.
// A nil token clears any previously set token.
// To set the connection token for a different connection ID, use Service.TokenEvent.
// Only valid for auth requests.
func (r *Request) TokenEvent(token interface{}) {
	r.s.event("conn."+r.cid+".token", tokenEvent{Token: token})
}

// success sends a successful response as a reply.
func (r *Request) success(result interface{}) {
	data, err := json.Marshal(successResponse{Result: result})
	if err != nil {
		r.error(ToError(err))
		return
	}

	r.reply(data)
}

// error sends an error response as a reply.
func (r *Request) error(e *Error) {
	data, err := json.Marshal(errorResponse{Error: e})
	if err != nil {
		data = responseInternalError
	}

	r.reply(data)
}

// reply sends an encoded payload to as a reply.
// If a reply is already sent, reply will panic.
func (r *Request) reply(payload []byte) {
	if r.replied {
		panic("res: response already sent on request")
	}
	r.replied = true
	r.s.Tracef("<== %s: %s", r.msg.Subject, payload)
	err := r.s.nc.Publish(r.msg.Reply, payload)
	if err != nil {
		r.s.Logf("error sending reply %s: %s", r.msg.Subject, err)
	}
}

func (r *Request) executeHandler() {
	// Recover from panics inside handlers
	defer func() {
		r.inGet = false
		v := recover()
		if v == nil {
			return
		}

		var str string

		switch e := v.(type) {
		case *Error:
			if !r.replied {
				r.error(e)
				// Return without logging as panicing with a *Error is considered
				// a valid way of sending an error response.
				return
			}
			str = e.Message
		case error:
			str = e.Error()
			if !r.replied {
				r.error(ToError(e))
			}
		case string:
			str = e
			if !r.replied {
				r.error(ToError(errors.New(e)))
			}
		default:
			str = fmt.Sprintf("%v", e)
			if !r.replied {
				r.error(ToError(errors.New(str)))
			}
		}

		r.s.Logf("error handling request %s: %s", r.msg.Subject, str)
	}()

	hs := r.hs

	switch r.rtype {
	case "access":
		if hs.Access == nil {
			// No handling. Assume the access requests is handled by other services.
			return
		}
		hs.Access(r)
	case "get":
		r.inGet = true
		switch hs.typ {
		case rtypeModel:
			hs.GetModel(r)
		case rtypeCollection:

			hs.GetCollection(r)
		default:
			r.reply(responseNotFound)
			return
		}
	case "call":
		if r.method == "new" {
			h := hs.New
			if h == nil {
				r.reply(responseMethodNotFound)
				return
			}
			h(r)
		} else {
			var h CallHandler
			if hs.Call != nil {
				h = hs.Call[r.method]
			}
			if h == nil {
				r.reply(responseMethodNotFound)
				return
			}
			h(r)
		}
	case "auth":
		var h AuthHandler
		if hs.Auth != nil {
			h = hs.Auth[r.method]
		}
		if h == nil {
			r.reply(responseMethodNotFound)
			return
		}
		h(r)
	default:
		r.s.Logf("unknown request type: %s", r.Type())
		return
	}

	if !r.replied {
		r.reply(responseMissingResponse)
	}
}
