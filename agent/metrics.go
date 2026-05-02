package main

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	gopsutilcpu "github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func (a *Agent) collectAndSendMetrics() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[metrics] Panic recovered: %v", r)
		}
	}()

	log.Printf("[metrics] Collecting host metrics...")

	payload := MetricsPayload{
		AgentName: a.cfg.AgentName,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// CPU
	cpuPercents, err := gopsutilcpu.Percent(500*time.Millisecond, false)
	if err != nil {
		log.Printf("[metrics] CPU error: %v", err)
	} else if len(cpuPercents) > 0 {
		payload.CPUPercent = cpuPercents[0]
		log.Printf("[metrics] CPU: %.1f%%", payload.CPUPercent)
	}

	// Memory
	memStat, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("[metrics] Memory error: %v", err)
	} else {
                payload.MemUsedMB = (memStat.Total - memStat.Available) / 1024 / 1024
		payload.MemTotalMB = memStat.Total / 1024 / 1024
		log.Printf("[metrics] Mem: %d/%d MB", payload.MemUsedMB, payload.MemTotalMB)
	}

	// Disk — try host mount first, fall back to /
	for _, path := range []string{"/host", "/"} {
		diskStat, err := disk.Usage(path)
		if err == nil {
			payload.DiskUsedGB  = diskStat.Used / 1024 / 1024 / 1024
			payload.DiskTotalGB = diskStat.Total / 1024 / 1024 / 1024
			log.Printf("[metrics] Disk (%s): %d/%d GB", path, payload.DiskUsedGB, payload.DiskTotalGB)
			break
		}
		log.Printf("[metrics] Disk error (%s): %v", path, err)
	}

	// Container metrics
	payload.Containers = a.collectContainerMetrics()
	log.Printf("[metrics] Containers: %d", len(payload.Containers))

	a.sendMessage(MsgMetrics, payload)
	log.Printf("[metrics] Done")
}

func (a *Agent) collectContainerMetrics() []ContainerMetrics {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[metrics] Container metrics panic: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := []ContainerMetrics{}

	containers, err := a.docker.client.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "lanmaster-")),
	})
	if err != nil {
		log.Printf("[metrics] ContainerList error: %v", err)
		return result
	}

	log.Printf("[metrics] Found %d containers matching 'lanmaster-'", len(containers))

	for _, c := range containers {
		stats, err := a.docker.client.ContainerStats(ctx, c.ID, false)
		if err != nil {
			log.Printf("[metrics] ContainerStats error: %v", err)
			continue
		}

		var s types.StatsJSON
		if err := json.NewDecoder(stats.Body).Decode(&s); err != nil {
			stats.Body.Close()
			continue
		}
		stats.Body.Close()

		cpuDelta    := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
		systemDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)

		// PercpuUsage is empty on cgroups v2 — fall back to OnlineCPUs,
		// then runtime.NumCPU() as last resort.
		numCPUs := len(s.CPUStats.CPUUsage.PercpuUsage)
		if numCPUs == 0 {
			numCPUs = int(s.CPUStats.OnlineCPUs)
		}
		if numCPUs == 0 {
			numCPUs = runtime.NumCPU()
		}

		cpuPercent := 0.0
		if systemDelta > 0 && cpuDelta > 0 {
			cpuPercent = (cpuDelta / systemDelta) * float64(numCPUs) * 100.0
		}

		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		log.Printf("[metrics] Container %s: CPU=%.1f%% MEM=%dMB", name, cpuPercent, s.MemoryStats.Usage/1024/1024)

		// On cgroups v2, subtract inactive_file to match what docker stats shows.
		// On cgroups v1 this key may not exist, in which case fall back to cache.
		memUsed := s.MemoryStats.Usage
		if inactiveFile, ok := s.MemoryStats.Stats["inactive_file"]; ok && memUsed > inactiveFile {
			memUsed -= inactiveFile
		} else if cache, ok := s.MemoryStats.Stats["cache"]; ok && memUsed > cache {
			memUsed -= cache
		}
		result = append(result, ContainerMetrics{
			ContainerName: name,
			CPUPercent:    cpuPercent,
			MemUsedMB:     memUsed / 1024 / 1024,
			MemLimitMB:    s.MemoryStats.Limit / 1024 / 1024,
		})
	}

	return result
}
