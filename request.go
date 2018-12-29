package res

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	Resource
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

type ResourceRequest interface {
	// Service returns the service instance
	Service() *Service

	/// Resource returns the resource name.
	ResourceName() string

	// PathParams returns parameters that are derived from the resource name.
	PathParams() map[string]string

	// Query returns the query part of the resource ID without the question mark separator.
	Query() string

	// ParseQuery parses the query and returns the corresponding values.
	// It silently discards malformed value pairs.
	// To check errors use url.ParseQuery(Query()).
	ParseQuery() url.Values

	// Event sends a custom event on the resource.
	// Will panic if the event is one of the pre-defined or reserved events,
	// "change", "add", "remove", "reaccess", or "unsubscribe".
	// For pre-defined events, the matching method, ChangeEvent, AddEvent,
	// RemoveEvent, or ReaccessEvent should be used instead.
	//
	// See the protocol specification for more information:
	// https://github.com/jirenius/resgate/blob/master/docs/res-service-protocol.md#events
	Event(event string, payload interface{})

	// ChangeEvents sends a change event with properties that has been changed
	// and their new values.
	// If props is empty, no event is sent.
	// Panics if the resource is not a Model.
	// The values must be serializable into JSON primitives, resource references,
	// or a delete action objects.
	// See the protocol specification for more information:
	//    https://github.com/jirenius/resgate/blob/master/docs/res-service-protocol.md#model-change-event
	ChangeEvent(props map[string]interface{})

	// AddEvent sends an add event, adding the value at index idx.
	// Panics if the resource is not a Collection.
	// The value must be serializable into a JSON primitive or resource reference.
	// See the protocol specification for more information:
	//    https://github.com/jirenius/resgate/blob/master/docs/res-service-protocol.md#collection-add-event
	AddEvent(value interface{}, idx int)

	// RemoveEvent sends a remove event, removing the value at index idx.
	// Panics if the resource is not a Collection.
	// See the protocol specification for more information:
	//    https://github.com/jirenius/resgate/blob/master/docs/res-service-protocol.md#collection-remove-event
	RemoveEvent(idx int)

	// ReaccessEvent sends a reaccess event to signal that the resource's access permissions has changed.
	// It will invalidate any previous access response sent for the resource.
	// See the protocol specification for more information:
	//    https://github.com/jirenius/resgate/blob/master/docs/res-service-protocol.md#reaccess-event
	ReaccessEvent()
}

// AccessRequest has methods for responding to access requests.
type AccessRequest interface {
	ResourceRequest
	Access(get bool, call string)
	AccessDenied()
	AccessGranted()
	NotFound()
	RawToken() json.RawMessage
	ParseToken(interface{})
}

// ModelRequest has methods for responding to model get requests.
type ModelRequest interface {
	ResourceRequest
	Model(model interface{})
	QueryModel(model interface{}, query string)
	NotFound()
}

// CollectionRequest has methods for responding to collection get requests.
type CollectionRequest interface {
	ResourceRequest
	Collection(collection interface{})
	QueryCollection(collection interface{}, query string)
	NotFound()
}

// CallRequest has methods for responding to call requests.
type CallRequest interface {
	ResourceRequest
	OK(result interface{})
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
}

// NewRequest has methods for responding to new call requests.
type NewRequest interface {
	ResourceRequest
	New(rid Ref)
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
}

// AuthRequest has methods for responding to auth requests.
type AuthRequest interface {
	ResourceRequest
	OK(result interface{})
	NotFound()
	MethodNotFound()
	InvalidParams(message string)
	Error(err *Error)
	RawParams() json.RawMessage
	RawToken() json.RawMessage
	ParseParams(interface{})
	ParseToken(interface{})
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

// OK sends a successful response for the call request.
// The result may be nil.
func (r *Request) OK(result interface{}) {
	r.success(result)
}

// Error sends a custom error response for the call request.
func (r *Request) Error(err *Error) {
	r.error(err)
}

// NotFound sends a system.notFound response for the access request.
func (r *Request) NotFound() {
	r.reply(responseNotFound)
}

// MethodNotFound sends a system.methodNotFound response for the call request.
func (r *Request) MethodNotFound() {
	r.reply(responseMethodNotFound)
}

// InvalidParams sends a system.invalidParams response.
// An empty message will be replaced will default to "Invalid parameters".
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
		r.success(okResponse{Get: get, Call: call})
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
// Only valid for get requests for a model query resource.
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
// Only valid for get requests for a query collection resource.
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
func (r *Request) New(rid Ref) {
	r.success(rid)
}

// ParseParams unmarshals the JSON encoded parameters and stores the result in p.
// If the request has no parameters, ParseParams does nothing.
// On any error, ParseParams panics with a system.invalidParams *Error.
func (r *Request) ParseParams(p interface{}) {
	err := json.Unmarshal(r.params, p)
	if err != nil {
		panic(&Error{Code: CodeInvalidParams, Message: err.Error()})
	}
}

// ParseToken unmarshals the JSON encoded token and stores the result in t.
// If the request has no token, ParseToken does nothing.
// On any error, ParseToken panics with a system.internalError *Error.
func (r *Request) ParseToken(t interface{}) {
	if r.params == nil {
		return
	}
	err := json.Unmarshal(r.params, t)
	if err != nil {
		panic(&Error{Code: CodeInvalidParams, Message: err.Error()})
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
		switch hs.typ {
		case rtypeUnset:
			r.reply(responseNotFound)
			return
		case rtypeModel:
			hs.GetModel(r)
		case rtypeCollection:
			hs.GetCollection(r)
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
		r.s.Logf("unknown request type: %s", r.Type)
		return
	}

	if !r.replied {
		r.reply(responseMissingResponse)
	}
}
