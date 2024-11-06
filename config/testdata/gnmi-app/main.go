package main

import (
    "io"
    "log"
    "net"
    "os/exec"
    "strconv"
    "strings"
    "time"

    "github.com/openconfig/gnmi/proto/gnmi"
    "google.golang.org/grpc"
)

type server struct {
    gnmi.UnimplementedGNMIServer
}

// Helper function to get CPU load
func getCPULoad() (string, error) {
    out, err := exec.Command("uptime").Output()
    if err != nil {
        return "", err
    }
    parts := strings.Fields(string(out))
    return parts[len(parts)-3], nil // Return the 1-minute load average
}

// Helper function to get Memory usage
func getMemUsage() (int64, error) {
    out, err := exec.Command("grep", "MemAvailable", "/proc/meminfo").Output()
    if err != nil {
        return 0, err
    }
    parts := strings.Fields(string(out))
    return strconv.ParseInt(parts[1], 10, 64)
}

// Implement the Subscribe method for Telegraf
func (s *server) Subscribe(stream gnmi.GNMI_SubscribeServer) error {
    log.Println("Received Subscribe request")

    for {
        // Receive request
        req, err := stream.Recv()
        if err == io.EOF {
            return nil
        }
        if err != nil {
            return err
        }

        log.Println("Subscription request:", req)

        // Fetch dynamic data
        cpuLoad, err := getCPULoad()
        if err != nil {
            return err
        }
        memAvailable, err := getMemUsage()
        if err != nil {
            return err
        }

        // Send a mock telemetry update response
        update := &gnmi.SubscribeResponse{
            Response: &gnmi.SubscribeResponse_Update{
                Update: &gnmi.Notification{
                    Prefix: &gnmi.Path{
                        Elem: []*gnmi.PathElem{
                            {Name: "system"},
                        },
                    },
                    Update: []*gnmi.Update{
                        {
                            Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "cpu-load"}}},
                            Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: cpuLoad}},
                        },
                        {
                            Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "mem-available"}}},
                            Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: memAvailable}},
                        },
                    },
                    Timestamp: time.Now().UnixNano(),
                },
            },
        }

        if err := stream.Send(update); err != nil {
            log.Printf("Error sending update: %v", err)
            return err
        }

        // Add a delay of 5 seconds between updates
        time.Sleep(5 * time.Second)
    }
}

func main() {
    lis, err := net.Listen("tcp", ":57400")
    if err != nil {
        log.Fatalf("Failed to listen on port 57400: %v", err)
    }

    grpcServer := grpc.NewServer()
    gnmi.RegisterGNMIServer(grpcServer, &server{})

    log.Println("gNMI server listening on port 57400")
    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Failed to serve gNMI: %v", err)
    }
}
