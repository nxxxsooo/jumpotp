package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/provider"
)

const defaultClientTimeout = provider.DefaultTimeout + 2*time.Second
const defaultConnectionTimeout = provider.DefaultTimeout + 5*time.Second

type Client struct {
	Socket  string
	Profile string
	Target  string
	Nonce   string
	Timeout time.Duration
}

func (c *Client) Code(ctx context.Context, _ string) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultClientTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	nonce := c.Nonce
	if nonce == "" {
		value := make([]byte, 16)
		if _, err := rand.Read(value); err != nil {
			return nil, &provider.Error{Kind: provider.Failed}
		}
		nonce = hex.EncodeToString(value)
		c.Nonce = nonce
		zero(value)
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(runCtx, "unix", c.Socket)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, &provider.Error{Kind: provider.TimedOut}
		}
		return nil, &provider.Error{Kind: provider.Unavailable}
	}
	defer connection.Close()
	deadline, ok := runCtx.Deadline()
	if ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := writeFrame(connection, request{Version: protocolVersion, Profile: c.Profile, Target: c.Target, Nonce: nonce}); err != nil {
		return nil, &provider.Error{Kind: provider.Failed}
	}
	var result response
	if err := readFrame(connection, &result); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, &provider.Error{Kind: provider.TimedOut}
		}
		return nil, &provider.Error{Kind: provider.Failed}
	}
	if result.Version != protocolVersion || result.Status != "ok" {
		return nil, responseError(result)
	}
	code := []byte(result.Code)
	result.Code = ""
	return code, nil
}

func responseError(result response) error {
	kind := provider.Kind(result.Kind)
	switch kind {
	case provider.Unavailable, provider.Locked, provider.Missing, provider.Ambiguous, provider.Invalid, provider.TimedOut, provider.Interrupted:
	default:
		kind = provider.Failed
	}
	if result.ElapsedSeconds != nil {
		return provider.NewMeasuredError(kind, *result.ElapsedSeconds)
	}
	return &provider.Error{Kind: kind}
}

func (c *Client) AwaitCode(ctx context.Context, item string) ([]byte, error) {
	for {
		code, err := c.Code(ctx, item)
		if err == nil {
			return code, nil
		}
		kind := provider.KindOf(err)
		if kind != provider.Unavailable && kind != provider.TimedOut {
			return nil, err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, &provider.Error{Kind: provider.Interrupted}
		case <-timer.C:
		}
	}
}
