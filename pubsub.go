// Copyright 2020 FOSS GmbH. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package srt

import (
	"fmt"
	"sync"
)

type PubSub interface {
	Publish(c Conn) error
	Subscribe(c Conn) error
}

type pubSub struct {
	incoming  chan *packet
	abort     chan struct{}
	lock      sync.Mutex
	listeners map[uint32]chan *packet
}

func NewPubSub() PubSub {
	pb := &pubSub{
		incoming:  make(chan *packet, 1024),
		listeners: make(map[uint32]chan *packet),
		abort:     make(chan struct{}),
	}

	go pb.broadcast()

	return pb
}

func (pb *pubSub) broadcast() {
	defer func() {
		log("exiting broadcast loop\n")
	}()

	for {
		select {
		case <-pb.abort:
			return
		case p := <-pb.incoming:
			pb.lock.Lock()
			for _, c := range pb.listeners {
				pp := p.Clone()

				select {
				case c <- pp:
				default:
					log("broadcast target queue is full\n")
				}
			}
			pb.lock.Unlock()
		}
	}
}

func (pb *pubSub) Publish(c Conn) error {
	var p *packet
	var err error
	conn, ok := c.(*srtConn)
	if !ok {
		return fmt.Errorf("The provided connection is not a SRT connection")
	}

	for {
		p, err = conn.ReadPacket()
		if err != nil {
			break
		}

		select {
		case pb.incoming <- p:
		default:
			log("incoming queue is full\n")
		}
	}

	close(pb.abort)

	return err
}

func (pb *pubSub) Subscribe(c Conn) error {
	l := make(chan *packet, 1024)
	socketId := c.SocketId()
	conn, ok := c.(*srtConn)
	if !ok {
		return fmt.Errorf("The provided connection is not a SRT connection")
	}

	pb.lock.Lock()
	pb.listeners[socketId] = l
	pb.lock.Unlock()

	defer func() {
		pb.lock.Lock()
		delete(pb.listeners, socketId)
		pb.lock.Unlock()
	}()

	for {
		select {
		case <-pb.abort:
			return EOF
		case p := <-l:
			if err := conn.WritePacket(p); err != nil {
				return err
			}
		}
	}

	return nil
}
