/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rpc

import (
	"errors"
	"io"
	"net/http"
	"runtime/debug"

	"configcenter/src/common/blog"
)

type Server struct {
	handlers       map[string]HandlerFunc
	streamHandlers map[string]HandlerStreamFunc
}

func NewServer() *Server {
	return &Server{
		handlers:       map[string]HandlerFunc{},
		streamHandlers: map[string]HandlerStreamFunc{},
	}
}

var connected = "200 Connected to CC RPC"

// ServeHTTP implements http.Handler interface
func (s *Server) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
		resp.WriteHeader(http.StatusMethodNotAllowed)
		io.WriteString(resp, "405 must CONNECT\n")
		return
	}
	conn, _, err := resp.(http.Hijacker).Hijack()
	if err != nil {
		blog.Errorf("rpc hijacking %s: %s", req.RemoteAddr, err.Error())
		return
	}
	io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")

	blog.V(3).Infof("connect from rpc client %s", req.RemoteAddr)
	session := NewServerSession(s, conn)
	if err = session.Run(); err != nil {
		blog.Errorf("dissconnect from rpc client %s: %s ", req.RemoteAddr, err.Error())
		return
	}
}

func (s *Server) Handle(name string, f HandlerFunc) {
	cmd, err := NewCommand(name)
	if err != nil {
		blog.Fatalf("command %s invalid: %s", name, err.Error())
	}
	s.handlers[cmd.String()] = f
}
func (s *Server) HandleStream(name string, f HandlerStreamFunc) {
	cmd, err := NewCommand(name)
	if err != nil {
		blog.Fatalf("command %s invalid: %s", name, err.Error())
	}
	s.streamHandlers[cmd.String()] = f
}

type ServerSession struct {
	srv       *Server
	wire      Wire
	responses chan *Message
	done      chan struct{}

	stream map[uint32]*StreamMessage
}

func NewServerSession(srv *Server, conn io.ReadWriteCloser) *ServerSession {
	return &ServerSession{
		srv:       srv,
		wire:      NewBinaryWire(conn),
		responses: make(chan *Message, 1024),
		done:      make(chan struct{}, 5),
	}
}

func (s *ServerSession) Run() error {
	go s.writeloop()
	defer func() {
		s.done <- struct{}{}
	}()
	return s.readloop()
}

func (s *ServerSession) Stop() {
	s.done <- struct{}{}
}

func (s *ServerSession) readFromWire(ret chan<- error) {
	msg, err := s.wire.Read()
	if err == io.EOF {
		ret <- err
		return
	} else if err != nil {
		blog.Errorf("Failed to read: %v", err)
		ret <- err
		return
	}

	switch msg.typz {
	case TypeRequest:
		call := msg.cmd.String()
		blog.V(3).Infof("[rpc server] calling [%s]", call)
		if handlerFunc, ok := s.srv.handlers[call]; ok {
			go s.handle(handlerFunc, msg)
		} else if handlerFunc, ok := s.srv.streamHandlers[call]; ok {
			go s.handleStream(handlerFunc, msg)
		} else {
			cmds := []string{}
			for cmd := range s.srv.handlers {
				cmds = append(cmds, cmd)
			}
			blog.V(3).Infof("[rpc server] command [%s] not found, existing command are: %#v", call, s.srv.handlers)
			s.pushResponse(msg, ErrCommandNotFount)
		}
	case TypeStream:
		stream, ok := s.stream[msg.seq]
		if ok {
			stream.input <- msg
		}
	case TypeStreamClose:
		stream, ok := s.stream[msg.seq]
		if ok {
			if len(msg.Data) > 0 {
				stream.err = errors.New(string(msg.Data))
			}
			stream.done <- struct{}{}
		}
	case TypePing:
		go s.handlePing(msg)
	default:
		blog.Warnf("[rpc server] unknow message type: %v", msg.typz)
	}
	ret <- nil
}

func (s *ServerSession) handle(f HandlerFunc, msg *Message) {
	defer func() {
		runtimeErr := recover()
		if runtimeErr != nil {
			stack := debug.Stack()
			blog.Errorf("command [%s] runtime error:\n%s", msg.cmd, stack)
		}
	}()
	result, err := f(msg)
	if encodeErr := msg.Encode(result); encodeErr != nil {
		blog.Errorf("EncodeData error: %s", encodeErr.Error())
	}
	s.pushResponse(msg, err)
}
func (s *ServerSession) handleStream(f HandlerStreamFunc, msg *Message) {
	stream, ok := s.stream[msg.seq]
	if !ok {
		stream = NewStreamMessage()
		s.stream[msg.seq] = stream

		go func() {
			for {
				select {
				case value := <-stream.output:
					nmsg := msg.copy()
					nmsg.typz = TypeStream
					err := nmsg.Encode(value)
					s.pushResponse(msg, err)
				case <-s.done:
					stream.err = ErrStreamStoped
					return
				case <-stream.done:
					return
				}
			}
		}()
		defer func() {
			runtimeErr := recover()
			if runtimeErr != nil {
				stack := debug.Stack()
				blog.Errorf("stream command [%s] runtime error:\n%s", msg.cmd, stack)
			}
		}()
		err := f(msg, stream)
		close(stream.input)
		close(stream.output)
		close(stream.done)
		s.pushResponse(msg, err)
	}
}

func (s *ServerSession) handlePing(msg *Message) {
	s.pushResponse(msg, nil)
}

func (s *ServerSession) pushResponse(msg *Message, err error) {
	msg.magicVersion = MagicVersion

	msg.typz = TypeResponse
	if err != nil {
		msg.typz = TypeError
		msg.Data = []byte(err.Error())
	}
	s.responses <- msg
}

func (s *ServerSession) readloop() error {
	ret := make(chan error)
	for {
		go s.readFromWire(ret)

		select {
		case err := <-ret:
			if err != nil {
				s.Stop()
				return err
			}
			continue
		case <-s.done:
			blog.Infof("[rpc server] RPC server stopped")
			return nil
		}
	}
}

func (s *ServerSession) writeloop() {
	for {
		select {
		case msg := <-s.responses:
			if err := s.wire.Write(msg); err != nil {
				blog.Errorf("Failed to write: %v", err)
			}
		case <-s.done:
			if queuelen := len(s.responses); queuelen > 0 {
				for queuelen > 0 {
					msg := <-s.responses
					if err := s.wire.Write(msg); err != nil {
						blog.Errorf("Failed to write: %v", err)
						break
					}
				}
			}
			msg := &Message{
				typz: TypeClose,
			}
			//Best effort to notify client to close connection
			s.wire.Write(msg)
			break
		}
	}
}