package main

import (
	"context"
	"github.com/Syleron/PulseHA/proto"
	"github.com/coreos/go-log/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/**
 * Server struct type
 */
type Server struct {
	sync.Mutex
	Status        proto.HealthCheckResponse_ServingStatus
	Last_response time.Time
	Members       []Member
	Config        *Config
	Log           log.Logger
	Server        *grpc.Server
	Listener      net.Listener
}

/**
 *
 */
func (s *Server) Check(ctx context.Context, in *proto.HealthCheckRequest) (*proto.HealthCheckResponse, error) {
	s.Lock()
	defer s.Unlock()
	switch in.Request {
	case proto.HealthCheckRequest_SETUP:
		log.Debug("Server:Check() - HealthCheckRequest Setup")
	case proto.HealthCheckRequest_STATUS:
		log.Debug("Server:Check() - HealthCheckRequest Status")
		return &proto.HealthCheckResponse{
			Status: proto.HealthCheckResponse_CONFIGURED,
		}, nil
	default:
	}
	return nil, nil
}

/**
 * Attempt to join a configured cluster
 */
func (s *Server) Join(ctx context.Context, in *proto.PulseJoin) (*proto.PulseJoin, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:Join() - Join Pulse cluster")

	// Are we configured?
	if !clusterCheck(s.Config) {
		// Create a client
		client := &Client{}

		// Attempt to connect
		err := client.Connect(in.Ip, in.Port, in.Hostname)

		if err != nil {
			return &proto.PulseJoin{
				Success: false,
				Message: "Unable to reach requested node. Join failed.",
			}, nil
		}

		//// Send our join request
		//r, err := client.SendJoin(&proto.PulseJoin{
		//
		//})

		// This is called by our local daemon/agent
		// It needs to send a request to the peer/node to get cluster details.
		// Add the node to the config
		// Notify our peers that a new member has joined
		return &proto.PulseJoin{
			Success: true,
		}, nil
	}

	return &proto.PulseJoin{
		Success: false,
		Message: "Unable to join as PulseHA is already in a cluster.",
	}, nil
}

/**
 * Break cluster / Leave from cluster
 */
func (s *Server) Leave(ctx context.Context, in *proto.PulseLeave) (*proto.PulseLeave, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:Leave() - Leave Pulse cluster")

	nodeTotal := len(s.Config.Nodes)

	// Are we even in a cluster?
	if nodeTotal == 0 {
		return &proto.PulseLeave{
			Success: false,
			Message: "Unable to leave as no cluster was found",
		}, nil
	}

	// Clear out the groups
	s.Config.Groups = map[string][]string{}
	// Clear out the nodes
	s.Config.Nodes = map[string]Node{}
	// save our config
	s.Config.Save()
	// Shutdown our main server
	s.Close()

	// Check to see if we are the only member in the cluster
	if nodeTotal == 1 {
		return &proto.PulseLeave{
			Success: true,
			Message: "Successfully dismantled cluster",
		}, nil
	}

	// We need to inform our peers that we have left!
	return &proto.PulseLeave{
		Success: true,
		Message: "Successfully left from cluster",
	}, nil
}

/**
 * Note: This will probably need to be replicated..
 */
func (s *Server) Create(ctx context.Context, in *proto.PulseCreate) (*proto.PulseCreate, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:Create() - Create Pulse cluster")
	// Method of first checking to see if we are in a cluster.
	if !clusterCheck(s.Config) {
		// we are not in an active cluster
		newNode := Node{
			IP:       in.BindIp,
			Port:     in.BindPort,
			IPGroups: make(map[string][]string, 0),
		}
		// Add the node to the nodes config
		s.Config.Nodes[GetHostname()] = newNode
		// Assign interface names to node
		for _, ifaceName := range getInterfaceNames() {
			if ifaceName != "lo" {
				// Add the interface to the node
				newNode.IPGroups[ifaceName] = make([]string, 0)
				// Create a new group name
				groupName := genGroupName(s.Config)
				// Create a group for the interface
				s.Config.Groups[groupName] = []string{}
				// assign the group to the interface
				GroupAssign(groupName, GetHostname(), ifaceName, s.Config)
			}
		}
		// Save the config
		s.Config.Save()
		// Reload the config
		//s.Config.Reload()
		// Setup the listener
		go s.Setup()
		// return if we were successful or not
		return &proto.PulseCreate{
			Success: true,
			Message: "Pulse cluster successfully created!",
		}, nil
	} else {
		return &proto.PulseCreate{
			Success: false,
			Message: "Pulse daemon is already in a configured cluster",
		}, nil
	}
}

/**
 * Add a new floating IP group
 * Note: This will probably need to be replicated..
 */
func (s *Server) NewGroup(ctx context.Context, in *proto.PulseGroupNew) (*proto.PulseGroupNew, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:NewGroup() - Create floating IP group")
	groupName, err := GroupNew(s.Config)
	if err != nil {
		return &proto.PulseGroupNew{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupNew{
		Success: true,
		Message: groupName + " successfully added.",
	}, nil
}

/**
 * Delete floating IP group
 */
func (s *Server) DeleteGroup(ctx context.Context, in *proto.PulseGroupDelete) (*proto.PulseGroupDelete, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:DeleteGroup() - Delete floating IP group")
	err := GroupDelete(in.Name, s.Config)
	if err != nil {
		return &proto.PulseGroupDelete{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupDelete{
		Success: true,
		Message: in.Name + " successfully deleted.",
	}, nil
}

/**
 *
 * Note: This will probably need to be replicated..
 */
func (s *Server) GroupIPAdd(ctx context.Context, in *proto.PulseGroupAdd) (*proto.PulseGroupAdd, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:GroupIPAdd() - Add IP addresses to group " + in.Name)
	err := GroupIpAdd(in.Name, in.Ips, s.Config)
	if err != nil {
		return &proto.PulseGroupAdd{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupAdd{
		Success: true,
		Message: "IP address(es) successfully added to " + in.Name,
	}, nil
}

/**
 *
 * Note: This will probably need to be replicated..
 */
func (s *Server) GroupIPRemove(ctx context.Context, in *proto.PulseGroupRemove) (*proto.PulseGroupRemove, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:GroupIPRemove() - Removing IPs from group " + in.Name)
	err := GroupIpRemove(in.Name, in.Ips, s.Config)
	if err != nil {
		return &proto.PulseGroupRemove{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupRemove{
		Success: true,
		Message: "IP address(es) successfully removed from " + in.Name,
	}, nil
}

/**
 *
 * Note: This will probably need to be replicated..
 */
func (s *Server) GroupAssign(ctx context.Context, in *proto.PulseGroupAssign) (*proto.PulseGroupAssign, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:GroupAssign() - Assigning group " + in.Group + " to interface " + in.Interface + " on node " + in.Node)
	err := GroupAssign(in.Group, in.Node, in.Interface, s.Config)
	if err != nil {
		return &proto.PulseGroupAssign{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupAssign{
		Success: true,
		Message: in.Group + " assigned to interface " + in.Interface + " on node " + in.Node,
	}, nil
}

/**
 *
 * Note: This will probably need to be replicated..
 */
func (s *Server) GroupUnassign(ctx context.Context, in *proto.PulseGroupUnassign) (*proto.PulseGroupUnassign, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:GroupUnassign() - Unassigning group " + in.Group + " from interface " + in.Interface + " on node " + in.Node)
	err := GroupUnassign(in.Group, in.Node, in.Interface, s.Config)
	if err != nil {
		return &proto.PulseGroupUnassign{
			Success: false,
			Message: err.Error(),
		}, nil
	}
	s.Config.Save()
	return &proto.PulseGroupUnassign{
		Success: true,
		Message: in.Group + " unassigned from interface " + in.Interface + " on node " + in.Node,
	}, nil
}

/**
 *
 */
func (s *Server) GroupList(ctx context.Context, in *proto.PulseGroupList) (*proto.PulseGroupList, error) {
	s.Lock()
	defer s.Unlock()
	log.Debug("Server:GroupList() - Getting groups and their IPs")

	list := make(map[string]*proto.Group)

	for name, ips := range s.Config.Groups {
		list[name] = &proto.Group{
			Ips: ips,
		}
	}

	return &proto.PulseGroupList{
		Groups: list,
	}, nil
}

/**
 * Setup pulse cli type
 */
func (s *Server) SetupCLI() {
	lis, err := net.Listen("tcp", "127.0.0.1:9443")
	if err != nil {
		log.Errorf("Failed to listen: %s", err)
	}
	grpcServer := grpc.NewServer()
	proto.RegisterRequesterServer(grpcServer, s)
	log.Info("CLI initialised on 127.0.0.1:9443")
	grpcServer.Serve(lis)
}

/**
 * Setup pulse server type
 */
func (s *Server) Setup() {
	// Only continue if we are in a configured cluster
	if !clusterCheck(s.Config) {
		return
	}

	var err error
	s.Listener, err = net.Listen("tcp", s.Config.LocalNode().IP+":"+s.Config.LocalNode().Port)

	if err != nil {
		log.Errorf("Failed to listen: %s", err)
		os.Exit(1)
	}

	if s.Config.Pulse.TLS {
		// Get project directory location
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Emergency(err)
		}
		if CreateFolder(dir + "/certs") {
			log.Warning("TLS keys are missing! Generating..")
			GenOpenSSL()
		}

		creds, err := credentials.NewServerTLSFromFile(dir+"/certs/server.crt", dir+"/certs/server.key")

		if err != nil {
			log.Error("Could not load TLS keys.")
			os.Exit(1)
		}

		s.Server = grpc.NewServer(grpc.Creds(creds))
	} else {
		s.Server = grpc.NewServer()
	}

	proto.RegisterRequesterServer(s.Server, s)

	log.Info("Pulse initialised on " + s.Config.LocalNode().IP + ":" + s.Config.LocalNode().Port)

	s.Server.Serve(s.Listener)
}

/**
 * Shutdown pulse server (not cli/cmd)
 */
func (s *Server) Close() {
	log.Debug("Shutting down server")
	s.Server.GracefulStop()
	s.Listener.Close()
}