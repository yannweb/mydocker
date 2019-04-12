package command

import (
	"fmt"
	"github.com/nicktming/mydocker/cgroups"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const (
	DEFAULTPATH = "/nicktming"
)

func Run(command string, tty bool, cg *cgroups.CroupManger, rootPath string, volumes []string, containerName string)  {
	//cmd := exec.Command(command)

	reader, writer, err := os.Pipe()
	if err != nil {
		log.Printf("Error: os.pipe() error:%v\n", err)
		return
	}

	//cmd := exec.Command("/proc/self/exe", "init", command)

	cmd := exec.Command("/proc/self/exe", "init")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}

	log.Printf("volume:%s\n", volumes)

	newRootPath := getRootPath(rootPath)
	cmd.Dir = newRootPath + "/busybox"
	if err := NewWorkDir(newRootPath, volumes); err == nil {
		cmd.Dir = newRootPath + "/mnt"
	}
	defer ClearWorkDir(newRootPath, volumes)


	cmd.ExtraFiles = []*os.File{reader}
	sendInitCommand(command, writer)

	id := ContainerUUID()
	if containerName == "" {
		containerName = id
	}
	if tty {
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
	} else {
		logFile, err := GetLogFile(containerName)
		if err != nil {
			log.Printf("GetLogFile error:%v\n", err)
		}
		cmd.Stdout = logFile
	}
	/**
	 *   Start() will not block, so it needs to use Wait()
	 *   Run() will block
	 */
	if err := cmd.Start(); err != nil {
		log.Printf("Run Start err: %v.\n", err)
		log.Fatal(err)
	}

	cg.Set()
	defer cg.Destroy()
	cg.Apply(strconv.Itoa(cmd.Process.Pid))

	RecordContainerInfo(strconv.Itoa(cmd.Process.Pid), containerName, id, command)

	// false 表明父进程(Run程序)无须等待子进程(Init程序,Init进程后续会被用户程序覆盖)
	if tty {
		cmd.Wait()
		DeleteContainerInfo(containerName)
	}
}

func sendInitCommand(command string, writer *os.File)  {
	_, err := writer.Write([]byte(command))
	if err != nil {
		log.Printf("writer.Write error:%v\n", err)
		return
	}
	writer.Close()
}


func getRootPath(rootPath string) string {
	log.Printf("rootPath:%s\n", rootPath)
	defaultPath := DEFAULTPATH
	if rootPath == "" {
		log.Printf("rootPath is empaty, set cmd.Dir by default: %s/mnt\n", defaultPath)
		rootPath = defaultPath
	}
	imageTar := rootPath + "/busybox.tar"
	exist, _ := PathExists(imageTar)
	if !exist {
		log.Printf("%s does not exist, set cmd.Dir by default: %s/mnt\n", imageTar, defaultPath)
		return defaultPath
	}
	imagePath := rootPath + "/busybox"
	exist, _ = PathExists(imageTar)
	if exist {
		os.RemoveAll(imagePath)
	}
	if err := os.Mkdir(imagePath, 0777); err != nil {
		log.Printf("mkdir %s err:%v, set cmd.Dir by default: %s/mnt\n", imagePath, err, defaultPath)
		return defaultPath
	}
	if _, err := exec.Command("tar", "-xvf", imageTar, "-C", imagePath).CombinedOutput(); err != nil {
		log.Printf("tar -xvf %s -C %s, err:%v, set cmd.Dir by default: %s/mnt\n", imageTar, imagePath, err, defaultPath)
		return defaultPath
	}
	return rootPath
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func ClearWorkDir(rootPath string, volumes []string)  {
	for _, volume := range volumes {
		ClearVolume(rootPath, volume)
	}
	ClearMountPoint(rootPath)
	ClearWriterLayer(rootPath)
}

func ClearVolume(rootPath, volume string)  {
	if volume != "" {
		containerMntPath   := rootPath + "/mnt"
		mountPath 	  := strings.Split(volume, ":")[1]
		containerPath := containerMntPath + mountPath
		if _, err := exec.Command("umount", "-f", containerPath).CombinedOutput(); err != nil {
			log.Printf("umount -f %s, err:%v\n", containerPath, err)
		}
		if err := os.RemoveAll(containerPath); err != nil {
			log.Printf("remove %s, err:%v\n", containerPath, err)
		}
	}
}

func ClearMountPoint(rootPath string)  {
	mnt := rootPath + "/mnt"
	if _, err := exec.Command("umount", "-f", mnt).CombinedOutput(); err != nil {
		log.Printf("mount -f %s, err:%v\n", mnt, err)
	}
	if err := os.RemoveAll(mnt); err != nil {
		log.Printf("remove %s, err:%v\n", mnt, err)
	}
}

func ClearWriterLayer(rootPath string) {
	writerLayer := rootPath + "/writerLayer"
	if err := os.RemoveAll(writerLayer); err != nil {
		log.Printf("remove %s, err:%v\n", writerLayer, err)
	}
}

func NewWorkDir(rootPath string, volumes []string) error {
	if err := CreateContainerLayer(rootPath); err != nil {
		return fmt.Errorf("CreateContainerLayer(%s) error: %v.\n", rootPath, err)
	}
	if err := CreateMntPoint(rootPath); err != nil {
		return fmt.Errorf("CreateMntPoint(%s) error: %v.\n", rootPath, err)
	}
	if err := SetMountPoint(rootPath); err != nil {
		return fmt.Errorf("SetMountPoint(%s) error: %v.\n", rootPath, err)
	}
	for _, volume := range volumes {
		if err := CreateVolume(rootPath, volume); err != nil {
			return fmt.Errorf("CreateVolume(%s, %s) error: %v.\n", rootPath, volume, err)
		}
	}
	return nil
}

func CreateVolume(rootPath, volume string) error {
	if volume != "" {
		containerMntPath := rootPath + "/mnt"
		hostPath 	:= strings.Split(volume, ":")[0]
		exist, _ := PathExists(hostPath)
		if !exist {
			if err := os.Mkdir(hostPath, 0777); err != nil {
				log.Printf("mkdir %s err:%v\n", hostPath, err)
				return fmt.Errorf("mkdir %s err:%v\n", hostPath, err)
			}
		}
		mountPath 	:= strings.Split(volume, ":")[1]
		containerPath := containerMntPath + mountPath
		if err := os.Mkdir(containerPath, 0777); err != nil {
			log.Printf("mkdir %s err:%v\n", containerPath, err)
			return fmt.Errorf("mkdir %s err:%v\n", containerPath, err)
		}
		dirs := "dirs=" + hostPath
		if _, err := exec.Command("mount", "-t", "aufs", "-o", dirs, "none", containerPath).CombinedOutput(); err != nil {
			log.Printf("mount -t aufs -o %s none %s, err:%v\n", dirs, containerPath, err)
			return fmt.Errorf("mount -t aufs -o %s none %s, err:%v\n", dirs, containerPath, err)
		}
	}
	return nil
}

func CreateContainerLayer(rootPath string) error {
	writerLayer := rootPath + "/writerLayer"
	if err := os.Mkdir(writerLayer, 0777); err != nil {
		log.Printf("mkdir %s err:%v\n", writerLayer, err)
		return fmt.Errorf("mkdir %s err:%v\n", writerLayer, err)
	}
	return nil 
}

func CreateMntPoint(rootPath string) error {
	mnt := rootPath + "/mnt"
	if err := os.Mkdir(mnt, 0777); err != nil {
		log.Printf("mkdir %s err:%v\n", mnt, err)
		return fmt.Errorf("mkdir %s err:%v\n", mnt, err)
	}
	return nil
}

func SetMountPoint(rootPath string) error {
	dirs := "dirs=" + rootPath + "/writerLayer:" + rootPath + "/busybox"
	mnt := rootPath + "/mnt"
	if _, err := exec.Command("mount", "-t", "aufs", "-o", dirs, "none", mnt).CombinedOutput(); err != nil {
		log.Printf("mount -t aufs -o %s none %s, err:%v\n", dirs, mnt, err)
		return fmt.Errorf("mount -t aufs -o %s none %s, err:%v\n", dirs, mnt, err)
	}
	return nil
}