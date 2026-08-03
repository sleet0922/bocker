package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// CmdList 列出已安装容器，解析 JSON 后以表格展示，比原生命令更丰富。
func CmdList() error {
	client := NewIncusClient()
	cs, err := client.ListContainers()
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		fmt.Println("暂无容器。使用 bocker install 安装一个。")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tNETWORK\tIPV4\tAUTOSTART\tPORTS")
	for i := range cs {
		c := &cs[i]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Name,
			strings.ToLower(c.Status),
			c.NetworkMode(),
			c.IPv4(),
			autostartBadge(c.Autostart()),
			portSummary(c.PortMappings()),
		)
	}
	return w.Flush()
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
