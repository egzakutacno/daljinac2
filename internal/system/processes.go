package system

import (
	"fmt"
	"sort"

	"github.com/shirou/gopsutil/v4/process"
)

type ProcessInfo struct {
	PID         int32   `json:"pid"`
	Name        string  `json:"name"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	WorkingDir  string  `json:"working_dir,omitempty"`
	CreateTime  int64   `json:"create_time,omitempty"`
}

func Processes() ([]ProcessInfo, error) {
	pids, err := process.Pids()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	result := make([]ProcessInfo, 0, len(pids))
	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}
		name, _ := p.Name()
		cpu, _ := p.CPUPercent()
		mem, _ := p.MemoryInfo()
		createTime, _ := p.CreateTime()

		info := ProcessInfo{
			PID:         pid,
			Name:        name,
			CPUPercent:  cpu,
			MemoryBytes: 0,
			CreateTime:  createTime,
		}
		if mem != nil {
			info.MemoryBytes = mem.RSS
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PID < result[j].PID
	})
	return result, nil
}

func KillProcess(pid int) error {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := p.Kill(); err != nil {
		return fmt.Errorf("kill process %d: %w", pid, err)
	}
	return nil
}

func KillByName(name string) (int, error) {
	procs, err := Processes()
	if err != nil {
		return 0, err
	}
	killed := 0
	for _, p := range procs {
		if p.Name == name {
			if err := KillProcess(int(p.PID)); err == nil {
				killed++
			}
		}
	}
	if killed == 0 {
		return 0, fmt.Errorf("no process named %s found", name)
	}
	return killed, nil
}

func FindProcess(name string) ([]ProcessInfo, error) {
	all, err := Processes()
	if err != nil {
		return nil, err
	}
	var result []ProcessInfo
	for _, p := range all {
		if p.Name == name {
			result = append(result, p)
		}
	}
	return result, nil
}
