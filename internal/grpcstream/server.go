package grpcstream

import (
	"context"
	"net"

	"github.com/abnos5525/go-streaming/internal/grpcstream/pb"
	"github.com/abnos5525/go-streaming/internal/stream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedControlServer
}

func Listen(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:9095"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g := grpc.NewServer()
	pb.RegisterControlServer(g, &Server{})
	reflection.Register(g)
	return g.Serve(lis)
}

func (s *Server) Start(_ context.Context, req *pb.StartRequest) (*pb.StartResponse, error) {
	if _, err := playPath("x", req.GetOutputType()); err != nil {
		return nil, err
	}
	meta, err := stream.Start(req.GetUrl(), int(req.GetOutputType()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	path, _ := playPath(meta.ID, req.GetOutputType())
	return &pb.StartResponse{Id: meta.ID, Url: path, OutputType: req.GetOutputType()}, nil
}

func (s *Server) Stop(_ context.Context, req *pb.StopRequest) (*pb.StopResponse, error) {
	if err := stream.Stop(req.GetUrl()); err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &pb.StopResponse{Message: "stopped"}, nil
}

func (s *Server) Status(context.Context, *pb.StatusRequest) (*pb.StatusResponse, error) {
	out := &pb.StatusResponse{}
	for _, m := range stream.Status() {
		out.Sessions = append(out.Sessions, &pb.Session{Id: m.ID, Url: m.URL, Ready: m.Ready})
	}
	return out, nil
}

func playPath(id string, outputType int32) (string, error) {
	switch outputType {
	case 6:
		return "/hls/" + id + "/index.m3u8", nil
	case 7:
		return "/webrtc/" + id + "/offer", nil
	default:
		return "", status.Error(codes.InvalidArgument, "output_type must be 6 or 7")
	}
}
