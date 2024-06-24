package pan_telemetry

import (
	"context"
	"strconv"
	"strings"

	"pan_telemetry/proto"

	"github.com/google/uuid"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "google.golang.org/protobuf/proto"
)

const CloudIdPrefix string = "cloud_"

func (st *STServer) DeviceSessionStart(ctx context.Context, req *proto.DeviceSessionStartRequest) (*proto.DeviceSessionStartResponse, error) {
	st.Log.Debug("DeviceSessionStart called from Serial[" + req.DeviceIdentity.Serial +
		"], Version[" + req.DeviceIdentity.PanosVersion +
		"], Hostname[" + req.DeviceIdentity.Hostname +
		"], IPv4[" + req.DeviceIdentity.Ipv4Address + "]")

	resp := &proto.DeviceSessionStartResponse{
		SessionId:          uuid.New().String(),
		SessionCreatedTime: timestamppb.Now(),
		DeviceSessionState: proto.DeviceSessionState_SESSION_INIT,
	}

	// Store the session-id for corresponding device-serial
	st.dblock.Lock()
	st.db[req.DeviceIdentity.Serial] = resp.SessionId
	st.dblock.Unlock()

	st.Log.Debug("Session [" + resp.SessionId + "] created.")

	return resp, nil
}

func (st *STServer) DiscoverDeviceCapability(stream proto.CloudTelemetryService_DiscoverDeviceCapabilityServer) error {
	for {
		deviceReq, err := stream.Recv()
		if err != nil {
			break
		}

		numDeviceCap := len(deviceReq.DeviceCapabilities)
		st.Log.Debugf("DiscoverDeviceCapability serial: %v, sessionID: %v, No. of capabilities: %v",
			deviceReq.Serial,
			deviceReq.SessionId,
			numDeviceCap)

		DumpDeviceCapability(st, deviceReq.GetDeviceCapabilities())

		for i := 0; i < numDeviceCap; i++ {
			cloudReqId := CloudIdPrefix + strconv.Itoa(i) + "_" + uuid.New().String()

			dc, err := pb.Marshal(deviceReq.DeviceCapabilities[i])
			if err != nil {
				st.Log.Errorf("DiscoverDeviceCapability Marshall err = %v", err)
				continue
			}

			// Store the capabilities for the request-id
			st.dblock.Lock()
			st.db[cloudReqId] = string(dc[:])
			st.dblock.Unlock()

			resp := &proto.DiscoverDeviceCapabilityResponse{
				SessionId:      deviceReq.SessionId,
				Serial:         deviceReq.Serial,
				CloudRequestId: cloudReqId,
			}

			err = stream.Send(resp)
			st.Log.Debugf("DiscoverDeviceCapability response: serial: %v, cloud-id: %v",
				deviceReq.Serial,
				resp.CloudRequestId)
		}
	}

	return nil
}

func (st *STServer) StreamDeviceChangeNotifications(stream proto.CloudTelemetryService_StreamDeviceChangeNotificationsServer) error {

	for {
		deviceReq, err := stream.Recv()
		if err != nil {
			st.Log.Errorf("StreamDeviceChangeNotifications stream recv err=%v", err)
			return err
		}

		st.Log.Debugf("StreamDeviceChangeNotifications recv sessionID: %v CloudReqID: %v, Resp-Len: %v",
			deviceReq.SessionId,
			deviceReq.CloudRequestId,
			len(deviceReq.DeviceSubscribeResponses))

		if len(deviceReq.DeviceSubscribeResponses) == 0 {
			st.dblock.Lock()
			v, found := st.db[deviceReq.CloudRequestId]
			st.dblock.Unlock()
			if !found {
				st.Log.Errorf("StreamDeviceChangeNotifications cannot find db entry for",
					"Device serial: %v, CloudRequest-ID: %v",
					deviceReq.Serial, deviceReq.CloudRequestId)
				continue
			}

			if strings.HasPrefix(deviceReq.CloudRequestId, CloudIdPrefix) {
				var p proto.DeviceCapabilities
				err = pb.Unmarshal([]byte(v), &p)
				if err != nil {
					st.Log.Errorf("StreamDeviceChangeNotifications Unmarshall err=%v", err)
					continue
				}

				sub := &gnmi.SubscribeRequest_Subscribe{
					Subscribe: &gnmi.SubscriptionList{
						Subscription: []*gnmi.Subscription{},
					},
				}

				for _, e := range p.DevicePaths {
					t := &gnmi.Subscription{
						Path:           e,
						SampleInterval: uint64(p.PublishInterval),
					}
					sub.Subscribe.Subscription = append(sub.Subscribe.Subscription, t)
				}

				sr := &gnmi.SubscribeRequest{
					Request: sub,
				}

				resp := &proto.StreamDeviceChangeNotificationsCloudMessage{
					SessionId:        deviceReq.SessionId,
					Serial:           deviceReq.Serial,
					CloudRequestId:   deviceReq.CloudRequestId,
					SubscribeRequest: sr,
					DataPushUrl:      "",
				}
				err = stream.Send(resp)
			} else {
				st.Log.Errorf("StreamDeviceChangeNotifications: Unknown CloudRequest-Id: %v",
					deviceReq.CloudRequestId)
			}

		} else {
			for _, r := range deviceReq.DeviceSubscribeResponses {
				st.respCh <- r
			}
		}
	}
}

func (st *STServer) DeviceSessionTerminate(ctx context.Context, req *proto.DeviceSessionTerminateRequest) (
	*proto.DeviceSessionTerminateResponse, error) {

	st.Log.Infof("DeviceSessionTerminate for serial: %v, SessionId: %v",
		req.Serial,
		req.SessionId)

	return nil, nil
}

func (st *STServer) QueryDeviceSessionStatistics(context.Context, *proto.QueryDeviceSessionStatisticsRequest) (
	*proto.QueryDeviceSessionStatisticsResponse, error) {
	return nil, nil
}

func (st *STServer) QueryServiceStatistics(context.Context, *proto.QueryServiceStatisticsRequest) (
	*proto.QueryServiceStatisticsResponse, error) {

	return nil, nil
}

func (st *STServer) ProcessSubscribeNotification() {
	for {
		select {
		case resp := <-st.respCh:
			LogSubscribeNotification(st, resp, st.PrettyPrintJson)
		}
	}
}
