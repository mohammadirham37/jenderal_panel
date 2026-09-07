//go:build !linux

package system

import "time"

func readCPU() float64                         { return 0 }
func readMemory() (uint64, uint64)             { return 0, 0 }
func readSwap() (uint64, uint64)               { return 0, 0 }
func readDisk() (uint64, uint64)               { return 0, 0 }
func readLoadAvg() (float64, float64, float64) { return 0, 0, 0 }
func readNetwork() (uint64, uint64)            { return 0, 0 }
func readUptime() time.Duration                { return 0 }
