package command

import (
	"log"
	"os"
	"os/exec"
	"syscall"
)

func Run(command string, tty bool)  {
	cmd := exec.Command(command)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}

	if tty {
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
	}
	/**
	 *   Start() will not block, so it needs to use Wait()
	 *   Run() will block
	 */
	if err := cmd.Start(); err != nil {
		log.Printf("Run Start err: %v.\n", err)
		log.Fatal(err)
	}
	cmd.Wait()
}
