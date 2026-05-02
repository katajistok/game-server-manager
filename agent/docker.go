package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type DockerManager struct {
	client    *client.Client
	logStreams map[int]context.CancelFunc
	network   string
	mu        sync.Mutex
}

func NewDockerManager() *DockerManager {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatalf("[docker] Failed to create Docker client: %v", err)
	}

	// Get network from env var, fallback to bridge
	agentNetwork := os.Getenv("AGENT_NETWORK")
	if agentNetwork == "" {
		agentNetwork = "bridge"
	}
	log.Printf("[docker] Game containers will use network: %s", agentNetwork)

	return &DockerManager{
		client:    cli,
		logStreams: make(map[int]context.CancelFunc),
		network:   agentNetwork,
	}
}

func (d *DockerManager) StartContainer(p StartServerPayload) (string, error) {
	ctx := context.Background()

	volumeName := p.ContainerName + "-data"
	_, err := d.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName,
	})
	if err != nil {
		log.Printf("[docker] Warning: could not create volume %s: %v", volumeName, err)
	} else {
		log.Printf("[docker] Volume ready: %s", volumeName)
	}

	log.Printf("[docker] Pulling image: %s", p.Image)
	reader, err := d.client.ImagePull(ctx, p.Image, types.ImagePullOptions{})
	if err != nil {
		return "", fmt.Errorf("image pull failed: %w", err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	d.removeContainerByName(ctx, p.ContainerName)

	env := []string{}
	for k, v := range p.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}

	for _, pm := range p.PortMappings {
		hostPort := p.Port + pm.HostPortOffset
		containerPort := pm.ContainerPort

		protocols := []string{}
		switch pm.Protocol {
		case "both":
			protocols = []string{"tcp", "udp"}
		case "tcp":
			protocols = []string{"tcp"}
		default:
			protocols = []string{"udp"}
		}

		for _, proto := range protocols {
			natPort := nat.Port(fmt.Sprintf("%d/%s", containerPort, proto))
			exposedPorts[natPort] = struct{}{}
			portBindings[natPort] = []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", hostPort)},
			}
		}
	}

	if len(p.PortMappings) == 0 {
		natPortUDP := nat.Port(fmt.Sprintf("%d/udp", p.Port))
		natPortTCP := nat.Port(fmt.Sprintf("%d/tcp", p.Port))
		exposedPorts[natPortUDP] = struct{}{}
		exposedPorts[natPortTCP] = struct{}{}
		portBindings[natPortUDP] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", p.Port)},
		}
		portBindings[natPortTCP] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", p.Port)},
		}
	}

	dataPath := p.DataPath
	if dataPath == "" {
		dataPath = "/data"
	}

	binds := []string{
		fmt.Sprintf("%s:%s", volumeName, dataPath),
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			d.network: {},
		},
	}

	log.Printf("[docker] Starting container %s on network %s", p.ContainerName, d.network)

	resp, err := d.client.ContainerCreate(ctx,
		&container.Config{
			Image:        p.Image,
			Env:          env,
			ExposedPorts: exposedPorts,
		},
		&container.HostConfig{
			PortBindings: portBindings,
			Binds:        binds,
			RestartPolicy: container.RestartPolicy{
				Name: "unless-stopped",
			},
		},
		networkingConfig,
		nil,
		p.ContainerName,
	)
	if err != nil {
		return "", fmt.Errorf("container create failed: %w", err)
	}

	if err := d.client.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("container start failed: %w", err)
	}

	return resp.ID, nil
}

func (d *DockerManager) StopContainer(containerName string) error {
	ctx := context.Background()

	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return fmt.Errorf("container not found: %s", containerName)
	}

	timeout := 10
	return d.client.ContainerStop(ctx, containers[0].ID, container.StopOptions{
		Timeout: &timeout,
	})
}

func (d *DockerManager) DeleteContainer(containerName string, volumeName string) error {
	ctx := context.Background()

	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	if err == nil && len(containers) > 0 {
		timeout := 10
		d.client.ContainerStop(ctx, containers[0].ID, container.StopOptions{Timeout: &timeout})
		d.client.ContainerRemove(ctx, containers[0].ID, types.ContainerRemoveOptions{Force: true})
		log.Printf("[docker] Removed container: %s", containerName)
	}

	if volumeName != "" {
		if err := d.client.VolumeRemove(ctx, volumeName, true); err != nil {
			log.Printf("[docker] Warning: could not remove volume %s: %v", volumeName, err)
		} else {
			log.Printf("[docker] Removed volume: %s", volumeName)
		}
	}

	return nil
}

func (d *DockerManager) StreamLogs(serverID int, containerName string, onLine func(string)) error {
	ctx, cancel := context.WithCancel(context.Background())

	d.mu.Lock()
	if existing, ok := d.logStreams[serverID]; ok {
		existing()
	}
	d.logStreams[serverID] = cancel
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.logStreams, serverID)
		d.mu.Unlock()
		cancel()
	}()

	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return fmt.Errorf("container not found: %s", containerName)
	}

	reader, err := d.client.ContainerLogs(ctx, containers[0].ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "50",
	})
	if err != nil {
		return err
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 8 {
			line = line[8:]
		}
		onLine(line)
	}

	return scanner.Err()
}

func (d *DockerManager) StopLogStream(serverID int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel, ok := d.logStreams[serverID]; ok {
		cancel()
		delete(d.logStreams, serverID)
		log.Printf("[docker] Stopped log stream for server %d", serverID)
	}
}

func (d *DockerManager) removeContainerByName(ctx context.Context, name string) {
	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil || len(containers) == 0 {
		return
	}
	timeout := 10
	d.client.ContainerStop(ctx, containers[0].ID, container.StopOptions{Timeout: &timeout})
	d.client.ContainerRemove(ctx, containers[0].ID, types.ContainerRemoveOptions{})
	log.Printf("[docker] Removed existing container: %s", name)
}
