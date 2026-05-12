package server

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"tbore/pkg/version"
)

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

func (status ServerStatus) String() string {
	var sb strings.Builder

	sb.WriteString("\n╔══════════════════════════════════════════════════════════════════════════════╗\n")
	sb.WriteString(fmt.Sprintf("║                          TBORE SERVER v%s                                 ║\n", version.Version))
	sb.WriteString("╠══════════════════════════════════════════════════════════════════════════════╣\n")

	uptimeStr := formatDuration(status.Uptime)
	sb.WriteString(fmt.Sprintf("║  Status: Running      │    Port: %s:%d    │    Uptime: %s        ║\n",
		status.Config.BindAddr, status.Config.Port, uptimeStr))

	totalTunnels := 0
	activeTunnels := 0
	for _, conn := range status.Connections {
		totalTunnels += len(conn.Tunnels)
		for _, tunnel := range conn.Tunnels {
			if tunnel.State == TunnelStateActive {
				activeTunnels++
			}
		}
	}

	sb.WriteString(fmt.Sprintf("║  Connections: %d/%d   │    Tunnels: %d/%d        │    Active: %d             ║\n",
		len(status.Connections), status.Config.MaxConnections,
		totalTunnels, status.Config.MaxTunnels*len(status.Connections),
		activeTunnels))

	sb.WriteString("╚══════════════════════════════════════════════════════════════════════════════╝\n")

	if len(status.Connections) == 0 {
		sb.WriteString("\nNo active connections.\n")
		return sb.String()
	}

	sb.WriteString("\nCONNECTION OVERVIEW\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")

	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CONN ID\tPROJECT\tREGION\tREMOTE ADDR")

	for _, conn := range status.Connections {
		project := conn.ClientInfo.Project
		if project == "" {
			project = "-"
		}
		region := conn.ClientInfo.Region
		if region == "" {
			region = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			conn.ID, project, region, conn.RemoteAddr)
	}

	w.Flush()

	sb.WriteString("\n")
	w = tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CONNECT TIME\tTUNNELS\tUPTIME")

	for _, conn := range status.Connections {
		uptime := formatDuration(time.Since(conn.ConnectTime))

		fmt.Fprintf(w, "%s\t%d\t%s\n",
			formatTime(conn.ConnectTime), len(conn.Tunnels), uptime)
	}

	w.Flush()

	sb.WriteString("\nDETAILED TUNNELS\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")

	for _, conn := range status.Connections {
		projectRegion := ""
		if conn.ClientInfo.Project != "" {
			projectRegion = conn.ClientInfo.Project
			if conn.ClientInfo.Region != "" {
				projectRegion += " (" + conn.ClientInfo.Region + ")"
			}
		}

		if projectRegion != "" {
			sb.WriteString(fmt.Sprintf("  %s | %s\n", conn.ID, projectRegion))
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", conn.ID))
		}

		if len(conn.Tunnels) == 0 {
			sb.WriteString("  └── (no tunnels)\n")
			continue
		}

		ports := make([]uint32, 0, len(conn.Tunnels))
		for port := range conn.Tunnels {
			ports = append(ports, port)
		}

		for i, port := range ports {
			tunnel := conn.Tunnels[port]
			stateIcon := "●"
			stateColor := "\033[32m"
			stateDesc := "UP"
			if tunnel.State == TunnelStateIdle {
				stateIcon = "○"
				stateColor = "\033[37m"
				stateDesc = "IDLE"
			} else if tunnel.State == TunnelStateError {
				stateIcon = "✗"
				stateColor = "\033[31m"
				stateDesc = "DOWN"
			}
			resetColor := "\033[0m"

			activeInfo := ""
			if tunnel.State != TunnelStateError && tunnel.ActiveConns > 0 {
				activeInfo = fmt.Sprintf(" (%d active)", tunnel.ActiveConns)
			}

			tunnelName := tunnel.Name
			if tunnelName == "" {
				tunnelName = fmt.Sprintf("tunnel-%d", tunnel.RemotePort)
			}

			localAddr := tunnel.LocalAddr
			if localAddr == "" || localAddr == "0.0.0.0" {
				localAddr = "127.0.0.1"
			}

			stateText := fmt.Sprintf("%s%s %s%s%s", stateColor, stateIcon, stateDesc, resetColor, activeInfo)

			if i == len(ports)-1 {
				sb.WriteString(fmt.Sprintf("  └── %-15s :%d  →  %s\t%s\n",
					tunnelName, tunnel.RemotePort, localAddr, stateText))
			} else {
				sb.WriteString(fmt.Sprintf("  ├── %-15s :%d  →  %s\t%s\n",
					tunnelName, tunnel.RemotePort, localAddr, stateText))
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
