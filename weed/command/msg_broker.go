package command

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"google.golang.org/grpc/reflection"

	"github.com/chrislusf/seaweedfs/weed/glog"
	"github.com/chrislusf/seaweedfs/weed/messaging"
	"github.com/chrislusf/seaweedfs/weed/pb"
	"github.com/chrislusf/seaweedfs/weed/pb/filer_pb"
	"github.com/chrislusf/seaweedfs/weed/pb/messaging_pb"
	"github.com/chrislusf/seaweedfs/weed/security"
	"github.com/chrislusf/seaweedfs/weed/util"
)

var (
	messageBrokerStandaloneOptions QueueOptions
)

type QueueOptions struct {
	filer *string
	port  *int
}

func init() {
	cmdMsgBroker.Run = runMsgBroker // break init cycle
	messageBrokerStandaloneOptions.filer = cmdMsgBroker.Flag.String("filer", "localhost:8888", "filer server address")
	messageBrokerStandaloneOptions.port = cmdMsgBroker.Flag.Int("port", 17777, "queue server gRPC listen port")
}

var cmdMsgBroker = &Command{
	UsageLine: "msg.broker [-port=17777] [-filer=<ip:port>]",
	Short:     "<WIP> start a message queue broker",
	Long: `start a message queue broker

	The broker can accept gRPC calls to write or read messages. The messages are stored via filer.
	The brokers are stateless. To scale up, just add more brokers.

`,
}

func runMsgBroker(cmd *Command, args []string) bool {

	util.LoadConfiguration("security", false)

	return messageBrokerStandaloneOptions.startQueueServer()

}

func (msgBrokerOpt *QueueOptions) startQueueServer() bool {

	filerGrpcAddress, err := pb.ParseFilerGrpcAddress(*msgBrokerOpt.filer)
	if err != nil {
		glog.Fatal(err)
		return false
	}

	grpcDialOption := security.LoadClientTLS(util.GetViper(), "grpc.msg_broker")
	cipher := false

	for {
		err = pb.WithGrpcFilerClient(filerGrpcAddress, grpcDialOption, func(client filer_pb.SeaweedFilerClient) error {
			resp, err := client.GetFilerConfiguration(context.Background(), &filer_pb.GetFilerConfigurationRequest{})
			if err != nil {
				return fmt.Errorf("get filer %s configuration: %v", filerGrpcAddress, err)
			}
			cipher = resp.Cipher
			return nil
		})
		if err != nil {
			glog.V(0).Infof("wait to connect to filer %s grpc address %s", *msgBrokerOpt.filer, filerGrpcAddress)
			time.Sleep(time.Second)
		} else {
			glog.V(0).Infof("connected to filer %s grpc address %s", *msgBrokerOpt.filer, filerGrpcAddress)
			break
		}
	}

	qs, err := messaging.NewMessageBroker(&messaging.MessageBrokerOption{
		Filers:             []string{*msgBrokerOpt.filer},
		DefaultReplication: "",
		MaxMB:              0,
		Port:               *msgBrokerOpt.port,
		Cipher:             cipher,
	}, grpcDialOption)

	// start grpc listener
	grpcL, err := util.NewListener(":"+strconv.Itoa(*msgBrokerOpt.port), 0)
	if err != nil {
		glog.Fatalf("failed to listen on grpc port %d: %v", *msgBrokerOpt.port, err)
	}
	grpcS := pb.NewGrpcServer(security.LoadServerTLS(util.GetViper(), "grpc.msg_broker"))
	messaging_pb.RegisterSeaweedMessagingServer(grpcS, qs)
	reflection.Register(grpcS)
	grpcS.Serve(grpcL)

	return true

}
