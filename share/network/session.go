package network

import (
	"io"
	"net"
	"strings"
	"sync"

	"github.com/ubis/Freya/share/encryption"
	"github.com/ubis/Freya/share/event"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/subpasswd"
)

// max buffer size
const MAX_RECV_BUFFER_SIZE = 4096

type Session struct {
	socket net.Conn
	buffer []byte

	Encryption encryption.Encryption
	UserIdx    uint16
	AuthKey    uint32
	DataEx     any
	Data       struct {
		AccountId     int32 // database account id
		Verified      bool  // version verification
		LoggedIn      bool  // auth verification
		CharVerified  bool  // character delete password verification
		SubPassword   *subpasswd.Details
		CharacterList []character.Character
	}

	PeriodicJobs map[string]*PeriodicTask
	jobMutex     sync.Mutex
	closeOnce    sync.Once
	closed       chan struct{}
}

// Starts session goroutine
func (s *Session) Start(table encryption.XorKeyTable) {
	// Keep a separate read buffer and accumulate data in s.buffer. TCP is a
	// stream: one Read may contain a partial packet or several packets.
	s.buffer = make([]byte, 0, MAX_RECV_BUFFER_SIZE)
	readBuffer := make([]byte, MAX_RECV_BUFFER_SIZE)
	// create map to store periodic tasks
	s.PeriodicJobs = make(map[string]*PeriodicTask)

	// init encryption
	s.Encryption = encryption.Encryption{}
	s.Encryption.Init(&table)

	firstPacket := true
	for {
		length, err := s.socket.Read(readBuffer)
		if length > 0 {
			s.buffer = append(s.buffer, readBuffer[:length]...)
		}

		closeSession := false
		for len(s.buffer) > 0 {
			if s.IsClosed() {
				break
			}

			// The first client packet has a fixed size. Subsequent packet
			// sizes are stored in the encrypted four-byte header.
			minimumHeader := 4
			if firstPacket {
				minimumHeader = encryption.Connect2SvrSize
			}
			if len(s.buffer) < minimumHeader {
				break
			}

			packetLength := s.Encryption.GetPacketSize(s.buffer)
			if packetLength < encryption.C2SHeaderSize {
				log.Errorf("Invalid packet length: %d", packetLength)
				closeSession = true
				break
			}
			if len(s.buffer) < packetLength {
				// The packet is split across TCP reads. Keep it for the next
				// iteration instead of closing the connection.
				break
			}

			// attempt to decrypt packet
			data, decryptErr := s.Encryption.Decrypt(s.buffer[:packetLength])

			if decryptErr != nil {
				log.Error("Error decrypting: " + decryptErr.Error())
				closeSession = true
				break
			}
			copy(s.buffer, s.buffer[packetLength:])
			s.buffer = s.buffer[:len(s.buffer)-packetLength]
			firstPacket = false

			// create new packet reader
			var reader = NewReader(data)

			// create new packet event argument
			arg := &PacketArgs{
				Session: s,
				Length:  int(reader.Size),
				Type:    int(reader.Type),
				Data:    data,
				Reader:  reader,
			}

			// trigger packet received event
			event.Trigger(event.PacketReceiveEvent, arg)
		}

		if s.IsClosed() {
			break
		}
		if closeSession {
			s.Close()
			break
		}
		if err != nil {
			if err != io.EOF {
				log.Error("Error reading: " + err.Error())
			}
			s.Close()
			break
		}
	}
}

// Sends specified data to the client
func (s *Session) Send(writer *Writer) {
	data := writer.Finalize()

	// encrypt data
	encrypt, err := s.Encryption.Encrypt(data)
	if err != nil {
		log.Error("Error encrypting packet: " + err.Error())
		return
	}

	// send it...
	length, err := s.socket.Write(encrypt)
	if err != nil {
		log.Error("Error sending packet: " + err.Error())
		return
	}

	// create new packet event argument
	arg := &PacketArgs{
		Session: s,
		Length:  length,
		Type:    writer.Type,
		Data:    data,
		Reader:  nil,
	}

	// trigger packet sent event
	event.Trigger(event.PacketSendEvent, arg)
}

// Returns session's remote endpoint
func (s *Session) GetEndPnt() string {
	return s.socket.RemoteAddr().String()
}

// Returns session's ip address
func (s *Session) GetIp() string {
	var ip = strings.Split(s.GetEndPnt(), ":")
	return ip[0]
}

// GetLocalEndPntIp returns local end point IP address.
// Local end point is server to which session is connected to.
func (s *Session) GetLocalEndPntIp() string {
	pnt := s.socket.LocalAddr().String()
	ip := strings.Split(pnt, ":")
	return ip[0]
}

// IsLocal checks if session's remote endpoint originated from private network.
func (s *Session) IsLocal() bool {
	return net.IP.IsPrivate(net.ParseIP(s.GetIp()))
}

// Closes session socket
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		if s.closed != nil {
			close(s.closed)
		}
		s.RemoveAllJobs()
		s.socket.Close()
		event.Trigger(event.ClientDisconnectEvent, s)
	})
}

// IsClosed reports whether the session has already started its close path.
func (s *Session) IsClosed() bool {
	if s.closed == nil {
		return false
	}

	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// AddJob adds a periodic task to the session's job list in a thread-safe manner.
// The job can be referenced and managed by its name.
func (s *Session) AddJob(name string, task *PeriodicTask) {
	s.jobMutex.Lock()
	defer s.jobMutex.Unlock()

	s.PeriodicJobs[name] = task
}

// RemoveJob stops and removes a periodic task from the session's job list
// based on its name in a thread-safe manner.
func (s *Session) RemoveJob(name string) {
	s.jobMutex.Lock()
	defer s.jobMutex.Unlock()

	if job, exists := s.PeriodicJobs[name]; exists {
		job.Stop()
		delete(s.PeriodicJobs, name)
	}
}

// RemoveAllJobs stops all periodic tasks and clears the job list
// for the session in a thread-safe manner.
func (s *Session) RemoveAllJobs() {
	s.jobMutex.Lock()
	defer s.jobMutex.Unlock()

	for name, job := range s.PeriodicJobs {
		job.Stop()
		delete(s.PeriodicJobs, name)
	}
}
