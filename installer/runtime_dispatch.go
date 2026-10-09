package main

// --reset-admin without --config is the administrator workflow; with --config it is the service worker command.
func runtimeServerMode(args []string) bool {
	for _, s := range args {
		switch s {
		case "--service", "--server-console", "--version", "--init", "--check-config", "--migrate-config", "--publish-apk":
			return true
		case "--config":
			return true
		}
	}
	return false
}
