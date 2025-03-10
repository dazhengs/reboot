package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
	"gopkg.in/yaml.v2"
)

var exePath = "C:/"

// Config struct to hold the YAML configuration
var config struct {
	AfterDays int    `yaml:"after_days"`
	At        string `yaml:"at"`
}

var serviceName = "RebootSchedulerService"

func loadConfig() {

	logFilePath := filepath.Join(exePath + "reboot_log.txt")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)

	configPath := filepath.Join(exePath, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := []byte("after_days: 300\nat: \"23:50\"")
		err = os.WriteFile(configPath, defaultConfig, 0644)
		if err != nil {
			log.Fatalf("Failed to create default config file: %v", err)
		}
		log.Println("Default config.yaml created.")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
}

// Calculate sleep duration until the next reboot time
func calculateSleepDuration() time.Duration {
	now := time.Now()
	restartTime, err := time.Parse("15:04", config.At)
	if err != nil {
		log.Fatalf("Failed to parse time: %v", err)
	}

	restartDateTime := time.Date(now.Year(), now.Month(), now.Day()+config.AfterDays, restartTime.Hour(), restartTime.Minute(), 0, 0, now.Location())
	return restartDateTime.Sub(now)
}

// Initiate system reboot
func reboot() {
	log.Println("Rebooting system...")
	cmd := exec.Command("shutdown", "/r", "/t", "10")
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to execute shutdown command: %v", err)
	}
	log.Println("System reboot initiated successfully.")
}

// Windows service struct
type RebootService struct{}

// Implement the Execute method to run the service
func (s *RebootService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	// Open a log file
	logFilePath := filepath.Join(exePath, "service_log.txt")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, 1
	}
	defer logFile.Close()

	// Set up logging to file
	multiWriter := io.MultiWriter(logFile)
	log.SetOutput(multiWriter)

	// Notify SCM that the service is starting
	status <- svc.Status{State: svc.StartPending}

	log.Println("Service is starting.")

	// Load configuration
	loadConfig()

	// Start service logic
	done := make(chan struct{})
	go func() {
		sleepDuration := calculateSleepDuration()
		log.Printf("System will reboot at: %s", time.Now().Add(sleepDuration).Format("2006-01-02 15:04:05"))

		// Wait until the reboot time or service stop request
		timer := time.NewTimer(sleepDuration)
		select {
		case <-timer.C:
			reboot()
		case <-done:
			timer.Stop()
		}
	}()

	// Notify SCM that the service is running
	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Listen for service stop/shutdown requests
loop:
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			}
		case <-done:
			break loop
		}
	}

	// Notify SCM that the service is stopping
	status <- svc.Status{State: svc.StopPending}
	log.Println("Service is stopping.")
	return false, 0
}

// Install and start the service
func installService() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		log.Println("Service already exists.")
		return
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: serviceName,
		Description: "Automatically reboots the system at a scheduled time.",
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer s.Close()

	// Set up event logging
	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		log.Fatalf("Failed to set up event log source: %v", err)
	}

	log.Println("Service installed successfully.")
}

// Remove the service
func uninstallService() {
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		log.Fatalf("Service does not exist.")
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		log.Fatalf("Failed to delete service: %v", err)
	}

	err = eventlog.Remove(serviceName)
	if err != nil {
		log.Fatalf("Failed to remove event log source: %v", err)
	}

	log.Println("Service uninstalled successfully.")
}

// Main function to install, uninstall, or run the service
func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			installService()
			return
		case "uninstall":
			uninstallService()
			return
		}
	}

	// Determine if running as a Windows service
	interactive, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("Failed to determine session type: %v", err)
	}

	if interactive {
		log.Println("This program should be run as a Windows service.")
		log.Println("Use 'install' to install the service and 'uninstall' to remove it.")
	}
	// Run as a Windows service
	err = svc.Run(serviceName, &RebootService{})
	if err != nil {
		log.Fatalf("Service failed: %v", err)
		return
	}
}
