package discord

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

type pipeConn struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

func newPipePair() (client io.ReadWriteCloser, server io.ReadWriteCloser) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	return &pipeConn{r: s2cR, w: c2sW}, &pipeConn{r: c2sR, w: s2cW}
}

func readTestFrame(r io.Reader) (uint32, []byte, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	buf := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, nil, err
		}
	}
	return op, buf, nil
}

func writeTestFrame(w io.Writer, op uint32, body []byte) error {
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], op)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[8:], body)
	_, err := w.Write(frame)
	return err
}

func TestConnectFallbackAcrossPipes(t *testing.T) {
	client := NewClient()
	done := make(chan struct{})

	client.dialPipe = func(slot int) (io.ReadWriteCloser, func() bool, error) {
		switch slot {
		case 0:
			c, s := newPipePair()
			go func() {
				defer s.Close()
				_, _, _ = readTestFrame(s)
				_ = writeTestFrame(s, OpClose, []byte(`{"code":4000,"message":"bad slot"}`))
			}()
			return c, nil, nil
		case 1:
			c, s := newPipePair()
			go func() {
				defer s.Close()
				defer close(done)
				op, body, err := readTestFrame(s)
				if err != nil || op != OpHandshake || !strings.Contains(string(body), `"1234"`) {
					t.Errorf("unexpected handshake on slot 1: op=%d body=%q err=%v", op, string(body), err)
					return
				}
				_ = writeTestFrame(s, OpFrame, []byte(`{"cmd":"DISPATCH","evt":"READY"}`))
			}()
			return c, nil, nil
		default:
			return nil, nil, errors.New("no pipe")
		}
	}

	if err := client.Connect("1234"); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	<-done
	if !client.IsConnected() {
		t.Fatal("expected IsConnected to be true")
	}
	_ = client.Close()
}

func TestSetActivityHandlesPingAndError(t *testing.T) {
	c, s := newPipePair()
	client := &Client{conn: c}

	serverErr := make(chan error, 1)
	go func() {
		defer s.Close()
		op, _, err := readTestFrame(s)
		if err != nil || op != OpFrame {
			serverErr <- errors.New("expected SET_ACTIVITY frame")
			return
		}
		if err := writeTestFrame(s, OpPing, []byte("ping1")); err != nil {
			serverErr <- err
			return
		}
		pongOp, pongBody, err := readTestFrame(s)
		if err != nil || pongOp != OpPong || string(pongBody) != "ping1" {
			serverErr <- errors.New("expected matching OpPong frame")
			return
		}
		if err := writeTestFrame(s, OpFrame, []byte(`{"cmd":"SET_ACTIVITY"}`)); err != nil {
			serverErr <- err
			return
		}

		// Second call returns an error response from Discord
		_, _, err = readTestFrame(s)
		if err != nil {
			serverErr <- err
			return
		}
		_ = writeTestFrame(s, OpFrame, []byte(`{"cmd":"SET_ACTIVITY","evt":"ERROR","data":{"code":4006,"message":"invalid asset"}}`))
		serverErr <- nil
	}()

	if err := client.SetActivity(42, &Activity{Details: "Playing as Sweden"}); err != nil {
		t.Fatalf("first SetActivity failed: %v", err)
	}

	err := client.SetActivity(42, &Activity{Details: "Playing as Sweden"})
	if err == nil || !strings.Contains(err.Error(), "4006") {
		t.Fatalf("expected error 4006, got %v", err)
	}
	if sErr := <-serverErr; sErr != nil {
		t.Fatalf("server check failed: %v", sErr)
	}
}

func TestIsConnectedDetectsBrokenPipe(t *testing.T) {
	c, s := newPipePair()
	defer s.Close()

	alive := true
	client := &Client{
		conn:       c,
		clientID:   "1234",
		checkAlive: func() bool { return alive },
	}
	if !client.IsConnected() {
		t.Fatal("expected connected while alive")
	}
	alive = false
	if client.IsConnected() {
		t.Fatal("expected IsConnected false after pipe dies")
	}
	if client.conn != nil {
		t.Fatal("expected dead conn to be closed and cleared")
	}
}

func TestActivityEqual(t *testing.T) {
	a := &Activity{
		Details:    "Playing as France",
		State:      "1444 • Singleplayer",
		Timestamps: &Timestamps{Start: 100},
		Assets:     &Assets{LargeImage: "fra", SmallImage: "paused"},
	}
	b := &Activity{
		Details:    "Playing as France",
		State:      "1444 • Singleplayer",
		Timestamps: &Timestamps{Start: 100},
		Assets:     &Assets{LargeImage: "fra", SmallImage: "paused"},
	}
	if !a.Equal(b) {
		t.Fatal("expected equal activities")
	}
	b.Assets.SmallImage = "at_war"
	if a.Equal(b) {
		t.Fatal("expected differing assets not equal")
	}
	if a.Equal(nil) || !(*Activity)(nil).Equal(nil) {
		t.Fatal("unexpected nil equality result")
	}
}
