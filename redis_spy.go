package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"

	"github.com/europelee/redis-spy/election"
	"github.com/europelee/redis-spy/utils"
)

type redisClient struct {
	redisConn net.Conn
	reader    *bufio.Reader
	isLeader  bool
	commandCh chan string
	errorCh   chan error
}

var redisAddrParam = *utils.New("127.0.0.1", 6379)
var raftBindAddr = *utils.New("127.0.0.1", 1000)
var raftDataDir = "/tmp/raft_data"
var raftPeers utils.NetAddrList

func (r *redisClient) initConn(redisAddrParam utils.NetAddr) bool {
	var err error
	addrStr := redisAddrParam.String()
	fmt.Println("addrStr:", addrStr)
	r.redisConn, err = net.Dial("tcp", addrStr)
	if err != nil {
		fmt.Println(err)
		return false
	}
	r.reader = bufio.NewReader(r.redisConn)
	r.isLeader = false
	r.commandCh = make(chan string)
	r.errorCh = make(chan error)
	return true
}

func (r *redisClient) finConn() {
	if r.redisConn != nil {
		r.redisConn.Close()
	}
	close(r.commandCh)
	close(r.errorCh)
}

func (r *redisClient) sendWatchRequest() {
	var buffer bytes.Buffer
	if r.redisConn == nil {
		return
	}
	monMsg := "monitor\r\n"
	err := binary.Write(&buffer, binary.BigEndian, []byte(monMsg))
	if err != nil {
		fmt.Println(err)
	}
	r.redisConn.Write(buffer.Bytes())
	buffer.Reset()
}

func (r *redisClient) recvWatchResponse(ch <-chan election.NodeState) bool {
	if r.redisConn == nil {
		r.errorCh <- errors.New("redis conn nil")
		return false
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		fmt.Println("read error:", err.Error())
		if err == io.EOF {
			r.errorCh <- errors.New("io.EOF")
			return false
		}
	}

	r.commandCh <- line
	return true
}

func (r *redisClient) loopRecv(ch <-chan election.NodeState) error {
	go func() {
		for {
			ret := r.recvWatchResponse(ch)
			if ret == false {
				break
			}
		}
	}()
	for {
		select {
		case nodeStat := <-ch:
			fmt.Println("nodeStat:", nodeStat)
			if nodeStat == election.Leader {
				r.isLeader = true
			}
			if nodeStat == election.Follower {
				r.isLeader = false
			}
		case command := <-r.commandCh:
			fmt.Println("command:", command)
		case err := <-r.errorCh:
			fmt.Println(err)
			goto ForEnd
		}
	}
ForEnd:
	return nil
}

func init() {
	flag.Var(&redisAddrParam, "redisAddr", "set redis address")
	flag.Var(&raftBindAddr, "raftBindAddr", "set raft bind address")
	flag.StringVar(&raftDataDir, "raftDataDir", raftDataDir, "set raft data directory")
	flag.Var(&raftPeers, "raftPeers", "set raft peers, default null")
}

func main() {
	flag.Parse()
	//log.Panicf("%s", raftPeers.String())
	electionInst := election.New(raftBindAddr, raftDataDir, raftPeers)
	go electionInst.Start()
	var redisClientInst = redisClient{redisConn: nil}
	redisClientInst.initConn(redisAddrParam)
	redisClientInst.sendWatchRequest()
	redisClientInst.loopRecv(electionInst.NodeStatCh)
	redisClientInst.finConn()
}
