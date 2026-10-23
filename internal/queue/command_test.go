package queue

import (
	"strings"
	"testing"
)

func TestValidateWorkerCommandRejectsUnitBreakers(t *testing.T) {
	for _, command := range []string{
		"php artisan queue:work\nUser=root",
		"php artisan queue:work\r\nExecStartPost=/bin/sh -c id",
		"php artisan queue:work\nUNITEOF\nid > /root/pwn\n",
		"php artisan queue:work\x00",
		"",
		"   ",
	} {
		if err := validateWorkerCommand(command); err == nil {
			t.Errorf("command %q was accepted", command)
		}
	}
	long := strings.Repeat("a", 1025)
	if err := validateWorkerCommand(long); err == nil {
		t.Error("an oversized command was accepted")
	}
	for _, command := range []string{
		"php artisan queue:work --queue=high,default --tries=3",
		"php artisan horizon",
	} {
		if err := validateWorkerCommand(command); err != nil {
			t.Errorf("valid command %q rejected: %v", command, err)
		}
	}
}
