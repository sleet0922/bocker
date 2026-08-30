package bocker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

type containerListItem struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Network     string `json:"network"`
	IPv4        string `json:"ipv4"`
	IPv6        string `json:"ipv6"`
	Memory      string `json:"memory"`
	MemoryBytes *int64 `json:"memory_bytes"`
	Domain      string `json:"domain"`
	Autostart   string `json:"autostart"`
	Ports       string `json:"ports"`
}

// CmdList 列出已安装容器；--json 提供给 GUI 和脚本使用。
func CmdList(args []string) error {
	jsonOutput, err := parseJSONOutputOption(args)
	if err != nil {
		return fmt.Errorf("container list: %w", err)
	}
	client := NewIncusClient()
	cs, err := client.ListContainers()
	if err != nil {
		return err
	}
	if jsonOutput {
		items := make([]containerListItem, 0, len(cs))
		for i := range cs {
			c := &cs[i]
			memory, memoryBytes := containerMemoryUsage(c)
			items = append(items, containerListItem{
				Name:        c.Name,
				Status:      strings.ToLower(c.Status),
				Network:     c.NetworkMode(),
				IPv4:        c.IPv4(),
				IPv6:        strings.Join(c.IPv6Addresses(), ","),
				Memory:      memory,
				MemoryBytes: memoryBytes,
				Domain:      c.Domain(),
				Autostart:   autostartBadge(c.Autostart()),
				Ports:       portSummary(c.PortMappings()),
			})
		}
		return json.NewEncoder(os.Stdout).Encode(items)
	}
	if len(cs) == 0 {
		fmt.Println("暂无容器。使用 'bocker template install' 安装一个。")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tNETWORK\tIPV4\tIPV6\tMEMORY\tDOMAIN\tAUTOSTART\tPORTS")
	for i := range cs {
		c := &cs[i]
		memory, _ := containerMemoryUsage(c)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Name,
			strings.ToLower(c.Status),
			c.NetworkMode(),
			c.IPv4(),
			strings.Join(c.IPv6Addresses(), ","),
			memory,
			c.Domain(),
			autostartBadge(c.Autostart()),
			portSummary(c.PortMappings()),
		)
	}
	return w.Flush()
}

func containerMemoryUsage(c *Container) (string, *int64) {
	if c == nil || !strings.EqualFold(c.Status, "running") || c.State == nil || c.State.MemoryUsage < 0 {
		return "-", nil
	}
	memoryBytes := c.State.MemoryUsage
	return humanSize(memoryBytes), &memoryBytes
}

func autostartBadge(v string) string {
	switch v {
	case "true":
		return "on"
	case "false":
		return "off"
	default:
		return "-"
	}
}
