# Using Windows system service to reboot at a certain  time interval

## first, build it

```sh
go mod tidy
go build
```

## second, run it, config it

```sh
.\reboot
```
this will generate a config.yaml file at the same directory to rebot.exe, for example:

```yaml
after_days: 300
at: "23:50"
```
If necessary you can modify the configuration file.

## third, install as a service and start it

```sh
.\reboot install
net start RebootSchedulerService
```

Done.

You can check event log to ensure it will restart at right time.