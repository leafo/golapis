package golapis

/*
#include <lua.h>
#include <lauxlib.h>
#include "lua_helpers.h"
*/
import "C"
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// =============================================================================
// TLS client handshake (sock:sslhandshake)
// =============================================================================
//
// sock:sslhandshake(reused_session?, server_name?, ssl_verify?, send_status_req?)
// upgrades a connected cosocket to TLS. Returns a session value (true when
// reused_session is false) or nil, err. Go's TLS stack resumes sessions by
// itself through a shared client session cache keyed by server name, so the
// session value is an opaque token: it is accepted when passed back but
// carries nothing. send_status_req (OCSP stapling) is accepted and ignored.
//
// ssl_verify checks the chain against the trusted certificates: the state's
// bundle from SetSSLTrustedCertificate (nginx's lua_ssl_trusted_certificate)
// when set, otherwise the system roots. server_name is sent as SNI and, like
// nginx, the hostname is only checked when a name is known: server_name, or
// the connect host when it isn't an IP address. Connecting by IP with no
// server_name verifies the chain alone.
//
// A pooled connection that was already handshaked succeeds immediately,
// whatever ssl_verify or server_name the new caller passes; nginx takes the
// same shortcut, so the pool key (host:port) is the caller's responsibility
// when verification settings differ.

// tlsClientConn is the cosocket's view of an upgraded connection. tls.Conn's
// Close writes a close_notify alert under a 5s deadline, which would stall
// the event loop when the peer isn't reading (sock:close, closeOnError,
// the finalizer and pool eviction all run there), so Close drops straight
// to the transport instead, as nginx's non-blocking shutdown does.
type tlsClientConn struct {
	*tls.Conn
}

func (c *tlsClientConn) Close() error {
	return c.Conn.NetConn().Close()
}

// tlsSessionCache is shared by every client handshake, like nginx's
// per-worker SSL session cache
var tlsSessionCache = tls.NewLRUClientSessionCache(256)

// SetSSLTrustedCertificate loads a PEM bundle that ssl_verify checks chains
// against, replacing the system roots. An empty path restores the system
// roots.
func (gls *GolapisLuaState) SetSSLTrustedCertificate(path string) error {
	if path == "" {
		gls.sslTrustedCerts = nil
		return nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return fmt.Errorf("no certificates found in %s", path)
	}
	gls.sslTrustedCerts = pool
	return nil
}

//export golapis_tcp_sslhandshake
func golapis_tcp_sslhandshake(L *C.lua_State) C.int {
	sock, sockID := getTCPSocketFromUserdata(L, 1)
	if sock == nil {
		C.lua_pushnil(L)
		pushGoString(L, "invalid socket")
		return 2
	}
	if !checkTCPSocketAffinity(L, sock, sockID) {
		return 2
	}

	nargs := C.lua_gettop(L)
	if nargs > 5 {
		pushGoString(L, fmt.Sprintf("ngx.socket sslhandshake: expecting 1 ~ 5 arguments (including the object), but seen %d", int(nargs)))
		return -2 // raise
	}

	if sock.closed {
		C.lua_pushnil(L)
		pushGoString(L, "closed")
		return 2
	}
	if sock.isRequestSocket {
		C.lua_pushnil(L)
		pushGoString(L, "not supported on the request socket")
		return 2
	}
	if !sock.connected || sock.conn == nil {
		C.lua_pushnil(L)
		pushGoString(L, "not connected")
		return 2
	}
	if !checkTCPSocketBusy(L, sock, true, true, true) {
		return 2
	}
	// Arguments: reused_session (anything; false means "don't want one"),
	// server_name (string), ssl_verify (truthy), send_status_req (ignored)
	wantSession := !(nargs >= 2 && C.lua_type(L, 2) == C.LUA_TBOOLEAN && C.lua_toboolean(L, 2) == 0)
	serverName := ""
	if nargs >= 3 && C.lua_type(L, 3) != C.LUA_TNIL {
		if C.lua_type(L, 3) != C.LUA_TSTRING {
			pushGoString(L, fmt.Sprintf("bad argument #2 to 'sslhandshake' (string expected, got %s)", luaTypeName(L, 3)))
			return -2 // raise
		}
		serverName = string(luaStringBytes(L, 3))
	}
	verify := nargs >= 4 && C.lua_toboolean(L, 4) != 0

	if _, ok := sock.conn.(*tlsClientConn); ok {
		// Already handshaked, e.g. a pooled TLS connection: succeed at once
		// like nginx, so clients can call sslhandshake after every connect
		if wantSession {
			newTLSSessionToken().PushToLua(L)
		} else {
			C.lua_pushboolean(L, 1)
		}
		return 1
	}
	if sock.readBufPos < len(sock.readBuf) || sock.activeUntilReader != nil {
		// Plaintext bytes already read ahead of the handshake can't be
		// reinterpreted as TLS records
		C.lua_pushnil(L)
		pushGoString(L, "unread data in the receive buffer")
		return 2
	}

	thread := getLuaThreadFromRegistry(L)
	if thread == nil || thread.curCo == nil {
		C.lua_pushnil(L)
		pushGoString(L, "sslhandshake: could not find thread context")
		return 2
	}

	cfg := &tls.Config{
		ClientSessionCache: tlsSessionCache,
		RootCAs:            thread.state.sslTrustedCerts,
		MinVersion:         tls.VersionTLS12,
	}
	if serverName != "" {
		cfg.ServerName = serverName
	} else if sock.host != "" && net.ParseIP(sock.host) == nil {
		cfg.ServerName = sock.host
	}
	switch {
	case !verify:
		cfg.InsecureSkipVerify = true
	case cfg.ServerName == "":
		// No hostname to check: verify the chain only, as nginx does when
		// server_name isn't given
		roots := cfg.RootCAs
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no peer certificate")
			}
			opts := x509.VerifyOptions{Roots: roots, Intermediates: x509.NewCertPool()}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		}
	}

	conn := sock.conn
	gen := sock.gen
	timeout := sock.connectTimeout
	issuingCo := thread.curCo

	if debugEnabled {
		debugLog("tcp.sslhandshake: id=%d server_name=%q verify=%v timeout=%v", sockID, cfg.ServerName, verify, timeout)
	}
	sock.connecting = true
	issuingCo.pendingIO = sock
	go func() {
		ctx := context.Background()
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		// Plaintext send/receive leave their per-call deadlines on the
		// transport and HandshakeContext doesn't reset them, so a STARTTLS
		// upgrade after a short-timeout read would fail at once
		conn.SetDeadline(time.Time{})
		tlsConn := tls.Client(conn, cfg)
		err := tlsConn.HandshakeContext(ctx)

		if err != nil {
			msg := normalizeTLSError(err)
			if debugEnabled {
				debugLog("tcp.sslhandshake: id=%d error=%s", sockID, msg)
			}
			thread.state.eventChan <- &StateEvent{
				Type:         EventResumeThread,
				Thread:       thread,
				Co:           issuingCo,
				ResumeValues: []interface{}{nil, msg},
				OnResume: func(event *StateEvent) {
					sock.connecting = false
					if event.Stale || sock.closed || sock.gen != gen {
						return
					}
					// A failed handshake leaves the connection unusable, as
					// in nginx: the socket is closed, timeout included
					sock.closeOnError()
				},
			}
			return
		}

		if debugEnabled {
			debugLog("tcp.sslhandshake: id=%d done version=%x resumed=%v", sockID, tlsConn.ConnectionState().Version, tlsConn.ConnectionState().DidResume)
		}
		thread.state.eventChan <- &StateEvent{
			Type:         EventResumeThread,
			Thread:       thread,
			Co:           issuingCo,
			ResumeValues: []interface{}{true},
			OnResume: func(event *StateEvent) {
				sock.connecting = false
				if event.Stale || sock.closed || sock.gen != gen {
					tlsConn.Close()
					event.ResumeValues = []interface{}{nil, "closed"}
					return
				}
				sock.conn = &tlsClientConn{Conn: tlsConn}
				if wantSession {
					event.ResumeValues = []interface{}{newTLSSessionToken()}
				}
			},
		}
	}()

	return C.lua_yield_wrapper(L, 0)
}

// tlsSessionToken is the opaque value sslhandshake returns as the session
type tlsSessionToken struct{}

func newTLSSessionToken() LuaPusher { return tlsSessionToken{} }

func (tlsSessionToken) PushToLua(L *C.lua_State) {
	C.lua_newtable_wrapper(L)
	pushGoString(L, "ssl session")
	C.lua_setfield(L, -2, cStrSessionKind)
}

var cStrSessionKind = C.CString("kind")

// normalizeTLSError maps handshake failures to short messages in the spirit
// of nginx's "handshake failed" and OpenSSL's verification errors
func normalizeTLSError(err error) string {
	if err == nil {
		return ""
	}
	if isTimeoutErr(err) {
		return "timeout"
	}
	var unknownCA x509.UnknownAuthorityError
	if errors.As(err, &unknownCA) {
		return "certificate verify failed: unable to get local issuer certificate"
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return "certificate verify failed: " + hostErr.Error()
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return "certificate verify failed: " + invalid.Error()
	}
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return "certificate verify failed: " + verifyErr.Err.Error()
	}
	return "handshake failed: " + normalizeNetError(err)
}
