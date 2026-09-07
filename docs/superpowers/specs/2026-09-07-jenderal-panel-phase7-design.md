# Jenderal Panel — Phase 7 (Docker) Design Spec

## Overview

Docker container, image, volume, and network management. Docker Compose support.

## 1. No Database Tables

All Docker state is queried live from Docker daemon. No panel DB tables needed.

## 2. Service (`internal/docker/service.go`)

```go
type Service struct {
    exec  executor.CommandExecutor
    audit *audit.Service
}
```

### Docker Management
- Install(ctx) — apt-get install docker.io
- Status(ctx) — systemctl status docker + docker info
- Start/Stop/Restart(ctx)

### Containers
- ListContainers(ctx, all bool) — docker ps / docker ps -a, parse JSON output
- StartContainer(ctx, id) / StopContainer / RestartContainer / RemoveContainer
- ContainerLogs(ctx, id, lines) — docker logs --tail
- InspectContainer(ctx, id) — docker inspect, return JSON

### Images
- ListImages(ctx) — docker images --format json
- PullImage(ctx, name) — docker pull
- RemoveImage(ctx, id) — docker rmi

### Volumes
- ListVolumes(ctx) — docker volume ls --format json
- CreateVolume(ctx, name) / RemoveVolume(ctx, name)

### Networks
- ListNetworks(ctx) — docker network ls --format json
- CreateNetwork(ctx, name) / RemoveNetwork(ctx, name)

### Docker Compose
- ComposeUp(ctx, path) — docker compose -f {path} up -d
- ComposeDown(ctx, path) — docker compose -f {path} down
- ComposeStatus(ctx, path) — docker compose -f {path} ps

## 3. Models

```go
type DockerStatus struct {
    Installed    bool   `json:"installed"`
    Running      bool   `json:"running"`
    Version      string `json:"version"`
    Containers   int    `json:"containers"`
    Images       int    `json:"images"`
}

type Container struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Image   string `json:"image"`
    Status  string `json:"status"`
    State   string `json:"state"`
    Ports   string `json:"ports"`
    Created string `json:"created"`
}

type DockerImage struct {
    ID         string `json:"id"`
    Repository string `json:"repository"`
    Tag        string `json:"tag"`
    Size       string `json:"size"`
    Created    string `json:"created"`
}

type DockerVolume struct {
    Name       string `json:"name"`
    Driver     string `json:"driver"`
    Mountpoint string `json:"mountpoint"`
}

type DockerNetwork struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Driver string `json:"driver"`
    Scope  string `json:"scope"`
}
```

## 4. API Routes

```
GET    /api/v1/docker/status
POST   /api/v1/docker/install
POST   /api/v1/docker/start
POST   /api/v1/docker/stop

GET    /api/v1/docker/containers
POST   /api/v1/docker/containers/{id}/start
POST   /api/v1/docker/containers/{id}/stop
POST   /api/v1/docker/containers/{id}/restart
DELETE /api/v1/docker/containers/{id}
GET    /api/v1/docker/containers/{id}/logs?lines=100
GET    /api/v1/docker/containers/{id}/inspect

GET    /api/v1/docker/images
POST   /api/v1/docker/images/pull        {name}
DELETE /api/v1/docker/images/{id}

GET    /api/v1/docker/volumes
POST   /api/v1/docker/volumes            {name}
DELETE /api/v1/docker/volumes/{name}

GET    /api/v1/docker/networks
POST   /api/v1/docker/networks           {name}
DELETE /api/v1/docker/networks/{name}

POST   /api/v1/docker/compose/up         {path}
POST   /api/v1/docker/compose/down       {path}
GET    /api/v1/docker/compose/status      ?path=
```

## 5. RBAC
`docker.view`, `docker.manage`

## 6. Frontend
`/docker` — tabs: Containers, Images, Volumes, Networks, Compose.

## 7. File Structure
```
internal/docker/
├── service.go
├── service_test.go
└── handler.go
```
