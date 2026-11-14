package main

func runPlaylist(args []string) error {
	switch args[1] {
	case "add":
	case "remove":
	case "list":
	case "show":
		return nil
	}
	return nil
}
