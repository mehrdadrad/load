package main

import (
	"log"

	pb "github.com/mehrdadrad/load/loadguide"
	"golang.org/x/net/context"
)

type server struct{}

func (s *server) SendResult(cx context.Context, in *pb.LoadResMsg) (*pb.Empty, error) {
	slaveResChn <- Result{
		ID:        in.ID,
		Status:    in.Status,
		URL:       in.Url,
		Timestamp: in.Timestamp,
	}
	return &pb.Empty{}, nil
}

func (s *server) SendLoad(cx context.Context, in *pb.PowerRequest) (*pb.LoadReply, error) {
	var respChn = make(chan pb.LoadReply, 1)
	reqLoadChn <- ReqLReply{in.Core, respChn}
	loadReply := <-respChn
	return &loadReply, nil
}

func (s *server) SendSignal(cx context.Context, in *pb.Signal) (*pb.Empty, error) {
	for i := 0; i <= lSlave.workers; i++ {
		shutdownChn <- struct{}{}
	}
	log.Print("Interrupt requested")
	return &pb.Empty{}, nil
}

func (s *server) Ping(cx context.Context, in *pb.WhoAmI) (*pb.WhoAmI, error) {
	log.Print("Got ping from master")
	masterAddr = in.Laddr
	slaveID = in.Raddr
	go runSlave(in)

	return &pb.WhoAmI{}, nil
}
