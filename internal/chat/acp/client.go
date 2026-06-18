package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Client implements a JSON-RPC 2.0 client over stdio pipes.
// It handles request/response correlation and notification dispatch.
type Client struct {
	stdin  io.Writer
	stdout io.Reader

	nextID    atomic.Int64
	pending   map[int64]chan *Response
	pendingMu sync.Mutex

	notifications chan *Notification
	requests      chan *Request

	done   chan struct{}
	closed atomic.Bool
	err    error
	errMu  sync.Mutex
}

func NewClient(stdin io.Writer, stdout io.Reader) *Client {
	c := &Client{
		stdin:         stdin,
		stdout:        stdout,
		pending:       make(map[int64]chan *Response),
		notifications: make(chan *Notification, 64),
		requests:      make(chan *Request, 16),
		done:          make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("client closed")
	}

	id := c.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	req := &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	respCh := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if err := c.send(req); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.getErr()
	case resp, ok := <-respCh:
		// Channel is closed (and drained) when the read loop exits, e.g. the
		// agent subprocess died. A closed channel yields a nil *Response, so
		// guard against it to avoid a nil pointer dereference.
		if !ok || resp == nil {
			return nil, c.getErr()
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	if c.closed.Load() {
		return fmt.Errorf("client closed")
	}

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
	}

	notif := &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}
	return c.send(notif)
}

// Respond sends a JSON-RPC response to an incoming request from the agent.
func (c *Client) Respond(id int64, result any, rpcErr *RPCError) error {
	if c.closed.Load() {
		return fmt.Errorf("client closed")
	}

	var rawResult json.RawMessage
	if result != nil && rpcErr == nil {
		var err error
		rawResult, err = json.Marshal(result)
		if err != nil {
			return fmt.Errorf("marshal result: %w", err)
		}
	}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  rawResult,
		Error:   rpcErr,
	}
	return c.send(resp)
}

// Notifications returns the channel for incoming notifications from the agent.
func (c *Client) Notifications() <-chan *Notification {
	return c.notifications
}

// Requests returns the channel for incoming method calls from the agent.
func (c *Client) Requests() <-chan *Request {
	return c.requests
}

// Done returns a channel that closes when the client's read loop exits.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Err returns the error that caused the client to stop, if any.
func (c *Client) Err() error {
	return c.getErr()
}

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	_, err = c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

func (c *Client) readLoop() {
	defer close(c.done)

	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		c.dispatch(line)
	}

	if err := scanner.Err(); err != nil {
		c.setErr(fmt.Errorf("read stdout: %w", err))
	} else {
		c.setErr(io.EOF)
	}

	c.pendingMu.Lock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = make(map[int64]chan *Response)
	c.pendingMu.Unlock()
}

func (c *Client) dispatch(line []byte) {
	// JSON-RPC 2.0 dispatch: Response has id+no method, Request has id+method, Notification has method+no id
	var peek struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if json.Unmarshal(line, &peek) != nil {
		return
	}

	if peek.ID != nil && peek.Method == "" {
		var resp Response
		if json.Unmarshal(line, &resp) != nil {
			return
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		c.pendingMu.Unlock()
		if ok {
			ch <- &resp
		}
		return
	}

	if peek.ID != nil && peek.Method != "" {
		var req Request
		if json.Unmarshal(line, &req) != nil {
			return
		}
		select {
		case c.requests <- &req:
		default:
		}
		return
	}

	if peek.Method != "" {
		var notif Notification
		if json.Unmarshal(line, &notif) != nil {
			return
		}
		select {
		case c.notifications <- &notif:
		default:
		}
		return
	}
}

func (c *Client) setErr(err error) {
	c.errMu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.errMu.Unlock()
}

func (c *Client) getErr() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err != nil {
		return c.err
	}
	return fmt.Errorf("client stopped")
}
