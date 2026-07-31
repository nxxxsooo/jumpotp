package broker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/mfa"
	"github.com/nxxxsooo/jumpotp/internal/provider"
	runtimepath "github.com/nxxxsooo/jumpotp/internal/runtime"
)

var ErrAlreadyRunning = errors.New("broker is already running")

type waiter struct {
	target   config.EffectiveTarget
	response chan response
}

type batch struct {
	waiters []*waiter
	timer   *time.Timer
}

type Server struct {
	config      *config.Config
	profile     string
	source      provider.Source
	window      time.Duration
	socket      string
	lease       string
	listener    *net.UnixListener
	mu          sync.Mutex
	batches     map[string]*batch
	seen        map[string]bool
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	handlerWait sync.WaitGroup
}

func NewServer(cfg *config.Config, profile string, source provider.Source, window time.Duration) (*Server, error) {
	if _, ok := cfg.Profiles[profile]; !ok {
		return nil, fmt.Errorf("profile %q is not configured", profile)
	}
	if source == nil {
		return nil, errors.New("broker provider is required")
	}
	if window <= 0 {
		window = 150 * time.Millisecond
	}
	dir, err := runtimepath.SecureDir()
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, "broker-"+profile+".sock")
	lease := filepath.Join(dir, "broker-"+profile+".lease")
	if len(socket) > 100 {
		return nil, errors.New("broker socket path exceeds the platform safety limit")
	}
	if err := prepareBrokerPaths(socket, lease); err != nil {
		return nil, err
	}
	address := &net.UnixAddr{Name: socket, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on broker socket: %w", err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		os.Remove(socket)
		return nil, fmt.Errorf("secure broker socket: %w", err)
	}
	identity, err := runtimepath.CurrentIdentity()
	if err != nil {
		listener.Close()
		os.Remove(socket)
		return nil, fmt.Errorf("read broker process identity: %w", err)
	}
	if err := runtimepath.WriteLease(lease, runtimepath.Lease{
		Kind:     "broker",
		Profile:  profile,
		Socket:   socket,
		Identity: identity,
	}); err != nil {
		listener.Close()
		os.Remove(socket)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		config:   cfg,
		profile:  profile,
		source:   source,
		window:   window,
		socket:   socket,
		lease:    lease,
		listener: listener,
		batches:  map[string]*batch{},
		seen:     map[string]bool{},
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

func SocketPath(profile string) (string, error) {
	dir, err := runtimepath.SecureDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "broker-"+profile+".sock")
	if len(path) > 100 {
		return "", errors.New("broker socket path exceeds the platform safety limit")
	}
	return path, nil
}

func (s *Server) Socket() string {
	return s.socket
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.ctx.Done():
		}
	}()
	for {
		connection, err := s.listener.AcceptUnix()
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				s.handlerWait.Wait()
				return nil
			}
			return fmt.Errorf("accept broker connection: %w", err)
		}
		s.handlerWait.Add(1)
		go func() {
			defer s.handlerWait.Done()
			s.handle(connection)
		}()
	}
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		for _, pending := range s.batches {
			pending.timer.Stop()
			for _, waiter := range pending.waiters {
				waiter.response <- response{Version: protocolVersion, Status: "error", Kind: string(provider.Interrupted), Message: "broker stopped"}
			}
		}
		s.batches = map[string]*batch{}
		s.mu.Unlock()
		closeErr = s.listener.Close()
		_ = os.Remove(s.socket)
		_ = os.Remove(s.lease)
	})
	return closeErr
}

func (s *Server) handle(connection *net.UnixConn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(defaultClientTimeout + 3*time.Second))
	var req request
	if err := readFrame(connection, &req); err != nil {
		_ = writeFrame(connection, response{Version: protocolVersion, Status: "error", Kind: string(provider.Failed), Message: "invalid request"})
		return
	}
	if req.Version != protocolVersion || req.Profile != s.profile || req.Nonce == "" || len(req.Nonce) > 128 {
		_ = writeFrame(connection, response{Version: protocolVersion, Status: "error", Kind: string(provider.Failed), Message: "request identity rejected"})
		return
	}
	target, err := s.config.Resolve(req.Profile, req.Target, "", false)
	if err != nil {
		_ = writeFrame(connection, response{Version: protocolVersion, Status: "error", Kind: string(provider.Failed), Message: "target rejected"})
		return
	}
	key := req.Profile + "/" + req.Target + "/" + req.Nonce
	wait := &waiter{target: target, response: make(chan response, 1)}
	if !s.enqueue(key, wait) {
		_ = writeFrame(connection, response{Version: protocolVersion, Status: "error", Kind: string(provider.Failed), Message: "duplicate readiness rejected"})
		return
	}
	select {
	case result := <-wait.response:
		_ = writeFrame(connection, result)
	case <-s.ctx.Done():
		_ = writeFrame(connection, response{Version: protocolVersion, Status: "error", Kind: string(provider.Interrupted), Message: "broker stopped"})
	}
}

func (s *Server) enqueue(connectionKey string, wait *waiter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[connectionKey] {
		return false
	}
	s.seen[connectionKey] = true
	group := wait.target.Provider + "\x00" + wait.target.Item
	current := s.batches[group]
	if current == nil {
		current = &batch{}
		s.batches[group] = current
		current.timer = time.AfterFunc(s.window, func() { s.flush(group) })
	}
	current.waiters = append(current.waiters, wait)
	return true
}

func (s *Server) flush(group string) {
	s.mu.Lock()
	current := s.batches[group]
	var first config.EffectiveTarget
	if current != nil && len(current.waiters) > 0 {
		first = current.waiters[0].target
	}
	s.mu.Unlock()
	if current == nil || first.Target == "" {
		return
	}
	code, err := s.source.Code(s.ctx, first.Item)
	if err == nil {
		err = mfa.ValidateCode(code, first.MFA)
		if err != nil {
			err = &provider.Error{Kind: provider.Invalid}
		}
	}
	s.mu.Lock()
	current = s.batches[group]
	delete(s.batches, group)
	var waiters []*waiter
	if current != nil {
		waiters = append(waiters, current.waiters...)
	}
	s.mu.Unlock()
	if current == nil {
		mfa.Zero(code)
		return
	}
	if err != nil {
		kind := provider.KindOf(err)
		for _, wait := range waiters {
			wait.response <- response{Version: protocolVersion, Status: "error", Kind: string(kind), Message: provider.SafeMessage(err)}
		}
		mfa.Zero(code)
		return
	}
	for _, wait := range waiters {
		wait.response <- response{Version: protocolVersion, Status: "ok", Code: string(code)}
	}
	mfa.Zero(code)
}

func prepareBrokerPaths(socket, lease string) error {
	if _, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
		return ErrAlreadyRunning
	}
	leaseValue, leaseErr := runtimepath.ReadLease(lease)
	if leaseErr == nil {
		if runtimepath.Matches(leaseValue.Identity) {
			return ErrAlreadyRunning
		}
		suffix := ".stale-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if _, err := os.Lstat(socket); err == nil {
			if err := os.Rename(socket, socket+suffix); err != nil {
				return fmt.Errorf("quarantine stale broker socket: %w", err)
			}
		}
		if err := os.Rename(lease, lease+suffix); err != nil {
			return fmt.Errorf("quarantine stale broker lease: %w", err)
		}
		return nil
	}
	if !errors.Is(leaseErr, os.ErrNotExist) {
		return fmt.Errorf("validate existing broker lease: %w", leaseErr)
	}
	if _, err := os.Lstat(socket); err == nil {
		return errors.New("broker socket exists without a validated lease")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect broker socket: %w", err)
	}
	return nil
}
