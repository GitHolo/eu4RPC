package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
)

const (
	OpHandshake uint32 = 0
	OpFrame     uint32 = 1
	OpClose     uint32 = 2
	OpPing      uint32 = 3
	OpPong      uint32 = 4
)

var (
	ErrDiscordNotFound = errors.New("discord IPC pipe not found (is Discord running?)")
	ErrNotConnected    = errors.New("not connected to Discord IPC")
)

type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type Activity struct {
	Details    string      `json:"details,omitempty"`
	State      string      `json:"state,omitempty"`
	Timestamps *Timestamps `json:"timestamps,omitempty"`
	Assets     *Assets     `json:"assets,omitempty"`
}

func (a *Activity) Equal(b *Activity) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Details != b.Details || a.State != b.State {
		return false
	}
	if (a.Timestamps == nil) != (b.Timestamps == nil) {
		return false
	}
	if a.Timestamps != nil && (a.Timestamps.Start != b.Timestamps.Start || a.Timestamps.End != b.Timestamps.End) {
		return false
	}
	if (a.Assets == nil) != (b.Assets == nil) {
		return false
	}
	if a.Assets != nil && *a.Assets != *b.Assets {
		return false
	}
	return true
}

type handshakePayload struct {
	V        int    `json:"v"`
	ClientID string `json:"client_id"`
}

type setActivityCommand struct {
	Cmd   string          `json:"cmd"`
	Args  setActivityArgs `json:"args"`
	Nonce string          `json:"nonce"`
}

type setActivityArgs struct {
	PID      int       `json:"pid"`
	Activity *Activity `json:"activity"`
}

type ipcResponse struct {
	Cmd  string `json:"cmd"`
	Evt  string `json:"evt"`
	Data struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"data"`
}

type Client struct {
	mu       sync.Mutex
	pipe     *os.File
	clientID string
	nonceSeq atomic.Uint64
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pipe != nil
}

func (c *Client) Connect(clientID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pipe != nil && c.clientID == clientID {
		return nil
	}
	c.closeLocked()

	var f *os.File
	var openErr error
	for i := 0; i < 10; i++ {
		pipePath := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
		f, openErr = openNamedPipe(pipePath)
		if openErr == nil {
			break
		}
	}
	if f == nil {
		return fmt.Errorf("%w: %v", ErrDiscordNotFound, openErr)
	}

	c.pipe = f
	c.clientID = clientID

	if err := c.sendLocked(OpHandshake, handshakePayload{V: 1, ClientID: clientID}); err != nil {
		c.closeLocked()
		return fmt.Errorf("discord handshake write failed: %w", err)
	}
	op, payload, err := c.readFrameLocked()
	if err != nil {
		c.closeLocked()
		return fmt.Errorf("discord handshake read failed: %w", err)
	}
	if op == OpClose {
		var resp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &resp)
		c.closeLocked()
		return fmt.Errorf("discord rejected handshake (code=%d): %s", resp.Code, resp.Message)
	}
	var resp ipcResponse
	if err := json.Unmarshal(payload, &resp); err == nil && resp.Evt == "ERROR" {
		c.closeLocked()
		return fmt.Errorf("discord handshake error (code=%d): %s", resp.Data.Code, resp.Data.Message)
	}

	return nil
}

func (c *Client) SetActivity(pid int, act *Activity) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pipe == nil {
		return ErrNotConnected
	}

	cmd := setActivityCommand{
		Cmd: "SET_ACTIVITY",
		Args: setActivityArgs{
			PID:      pid,
			Activity: act,
		},
		Nonce: fmt.Sprintf("eu4rpc-%d", c.nonceSeq.Add(1)),
	}
	if err := c.sendLocked(OpFrame, cmd); err != nil {
		c.closeLocked()
		return err
	}

	op, payload, err := c.readFrameLocked()
	if err != nil {
		c.closeLocked()
		return err
	}
	if op == OpClose {
		c.closeLocked()
		return errors.New("discord closed IPC connection")
	}

	var resp ipcResponse
	if err := json.Unmarshal(payload, &resp); err == nil && resp.Evt == "ERROR" {
		return fmt.Errorf("discord SET_ACTIVITY error (code=%d): %s", resp.Data.Code, resp.Data.Message)
	}
	return nil
}

func (c *Client) ClearActivity(pid int) error {
	return c.SetActivity(pid, nil)
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *Client) closeLocked() {
	if c.pipe != nil {
		_ = c.pipe.Close()
		c.pipe = nil
	}
	c.clientID = ""
}

func (c *Client) sendLocked(op uint32, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], op)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[8:], body)

	written := 0
	for written < len(frame) {
		n, err := c.pipe.Write(frame[written:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		written += n
	}
	return nil
}

func (c *Client) readFrameLocked() (uint32, []byte, error) {
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(c.pipe, hdr[:]); err != nil {
			return 0, nil, err
		}
		op := binary.LittleEndian.Uint32(hdr[0:4])
		length := binary.LittleEndian.Uint32(hdr[4:8])
		if length > 1024*1024 {
			return 0, nil, fmt.Errorf("discord frame too large: %d", length)
		}
		payload := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(c.pipe, payload); err != nil {
				return 0, nil, err
			}
		}
		if op == OpPing {
			_ = c.writeRawLocked(OpPong, payload)
			continue
		}
		return op, payload, nil
	}
}

func (c *Client) writeRawLocked(op uint32, payload []byte) error {
	frame := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(frame[0:4], op)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	_, err := c.pipe.Write(frame)
	return err
}

func openNamedPipe(pipePath string) (*os.File, error) {
	p16, err := syscall.UTF16PtrFromString(pipePath)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(
		p16,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	if h == syscall.InvalidHandle || h == 0 {
		return nil, errors.New("invalid pipe handle")
	}
	return os.NewFile(uintptr(h), pipePath), nil
}
