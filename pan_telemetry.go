package pan_telemetry

import (
	"crypto/tls"
	"net"
	"pan_telemetry/proto"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/openconfig/gnmi/proto/gnmi"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

type STServer struct {
	proto.UnimplementedCloudTelemetryServiceServer

	respCh chan *gnmi.SubscribeResponse

	db     map[string]string
	dblock sync.RWMutex

	acc		telegraf.Accumulator
	Log		telegraf.Logger

	ListenAddr 		string 	`toml:"listen_address"`
	TLSCert    		string 	`toml:"tls_cert"`
	TLSKey     		string 	`toml:"tls_key"`
	PrettyPrintJson bool 	`toml:"prettyprint_json"`
}

func init() {
	inputs.Add("pan_telemetry", func() telegraf.Input { return NewSTServer() })
}

func (st *STServer) Description() string {
	return "PANW POC telegraf input plugin for Dial out Streaming Telemetry"
}

func (st *STServer) SampleConfig() string {
	return "No specific configuration"
}

// The Gather function gets called on an interval that is set in the configuration file
func (st *STServer) Gather(acc telegraf.Accumulator) error {
	// fmt.Println("pan_telemetry, Gather called")
	// acc.AddFields(measurement, fields, tags, time)
	// acc.AddFields("Logging", Lmap, nil)
	// acc.AddFields("Config", Cmap, nil)
	// acc.AddFields("Reporting", Rmap, nil)
	return nil
}

// Start PANW st-plugin service
func (st *STServer) Start(acc telegraf.Accumulator) error {
	st.Log.Infof("Telegraf start panw st dialout server [%s]", st.ListenAddr)
	st.acc = acc
	go startSTServer(st)
	return nil
}

// Stop server
// stops the services and closes any necessary channels and connections
// Metrics should not be written out to the accumulator once stop returns, so
// Stop() should stop reading and wait for any in-flight metrics to write out
// to the accumulator before returning.
func (st *STServer) Stop() {
	st.Log.Info("Stopped PANW ST Dial-out server")
}

func NewSTServer() *STServer {
	st := STServer{
		db: make(map[string]string),
	}

	st.respCh = make(chan *gnmi.SubscribeResponse, 1000)

	go st.ProcessSubscribeNotification()

	return &st
}

func startSTServer(st *STServer) {
	lis, err := net.Listen("tcp", st.ListenAddr)
	if err != nil {
		st.Log.Errorf("Failed to listen: %v", err)
		panic(err)
	}

	serverCert, err := tls.LoadX509KeyPair(st.TLSCert, st.TLSKey)
	if err != nil {
		st.Log.Errorf("Error loading certificate, key pair. Err = %v", err)
		panic(err)
	}

	config := &tls.Config {
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}

	cred := credentials.NewTLS(config)

	grpcServer := grpc.NewServer(grpc.Creds(cred))
	reflection.Register(grpcServer)
	proto.RegisterCloudTelemetryServiceServer(grpcServer, st)

	if err := grpcServer.Serve(lis); err != nil {
		panic(err)
	}

	st.Log.Info("Exiting PANW Dial-out ST server")
}

