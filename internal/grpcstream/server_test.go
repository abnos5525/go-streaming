package grpcstream

import (
	"context"
	"net"
	"testing"

	"github.com/abnos5525/go-streaming/internal/grpcstream/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestRejectsBadURL(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	pb.RegisterControlServer(g, &Server{})
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)

	conn, err := grpc.NewClient("passthrough:///buf",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := pb.NewControlClient(conn)

	_, err = client.Start(context.Background(), &pb.StartRequest{Url: "http://nope", OutputType: 6})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("start code=%v err=%v", status.Code(err), err)
	}
	st, err := client.Status(context.Background(), &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.GetSessions()) != 0 {
		t.Fatalf("sessions=%d", len(st.GetSessions()))
	}
}
