package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ddx-510/Morpho/chat"
	"github.com/ddx-510/Morpho/event"
)

// ANSI
const (
	reset     = "\033[0m"
	bold      = "\033[1m"
	italic    = "\033[3m"
	green     = "\033[32m"
	yellow    = "\033[33m"
	red       = "\033[31m"
	blue      = "\033[34m"
	orange    = "\033[38;5;208m"
	peach     = "\033[38;5;216m"
	lavender  = "\033[38;5;183m"
	teal      = "\033[38;5;80m"
	grayFg    = "\033[38;5;242m"
	lightGray = "\033[38;5;248m"
)

// registerTerminalHooks wires up ANSI-colored event logging to stdout.
// Used by both terminal mode and web mode.
func registerTerminalHooks(app *chat.App) {
	app.Bus.On(event.Thinking, func(ev event.Event) {
		fmt.Printf("  %s%s %s%s\n", grayFg+italic, "", ev.Content, reset)
	})
	app.Bus.On(event.ToolUse, func(ev event.Event) {
		args := ev.Meta["args"]
		if len(args) > 80 {
			args = args[:80] + "..."
		}
		fmt.Printf("  %s%s ▸ %s%s %s%s%s\n", lavender+bold, "", ev.Meta["tool"], reset, grayFg, args, reset)
	})
	app.Bus.On(event.ToolResult, func(ev event.Event) {
		content := ev.Content
		if content == "" {
			return
		}
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		fmt.Printf("  %s  %s%s\n", grayFg, content, reset)
	})
	app.Bus.On(event.TickStart, func(ev event.Event) {
		fmt.Printf("\n  %s%s ━━ tick %s %s\n", orange+bold, "", ev.Meta["tick"], reset)
	})
	app.Bus.On(event.AgentSpawn, func(ev event.Event) {
		fmt.Printf("  %s + %s%s%s at %s\n", green, bold, ev.Meta["agent"], reset, ev.Meta["point"])
	})
	app.Bus.On(event.AgentDiff, func(ev event.Event) {
		fmt.Printf("  %s ~ %s%s %s→ %s%s\n", yellow, ev.Meta["agent"], reset, grayFg, roleColor(ev.Meta["role"]), reset)
	})
	app.Bus.On(event.AgentDone, func(ev event.Event) {
		fmt.Printf("  %s ✓ %s%s %s%s@%s%s\n", green, roleColor(ev.Meta["role"]), reset, grayFg, ev.Meta["agent"], ev.Meta["point"], reset)
	})
	app.Bus.On(event.AgentDeath, func(ev event.Event) {
		fmt.Printf("  %s ✗ %s%s %sdied%s\n", red, ev.Meta["agent"], reset, grayFg, reset)
	})
	app.Bus.On(event.RunComplete, func(ev event.Event) {
		fmt.Printf("\n  %s%s ✓ complete%s  %s%s findings in %s%s\n",
			green+bold, "", reset, grayFg, ev.Meta["findings"], ev.Meta["elapsed"], reset)
	})
	app.Bus.On(event.AssistantMessage, func(ev event.Event) {
		if ev.Content == "" {
			return
		}
		content := ev.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		fmt.Printf("  %s│%s %s\n", teal, reset, content)
	})
}

// RunTerminal starts the interactive terminal UI.
func RunTerminal(app *chat.App) {
	// Header
	fmt.Printf("\n  %s%s morpho %s\n", bold+peach, "", reset)
	fmt.Printf("  %s%s%s\n", grayFg, strings.Repeat("─", 50), reset)
	fmt.Printf("  %sprovider  %s%s%s\n", grayFg, lightGray, app.Provider.Name(), reset)
	fmt.Printf("  %sdir       %s%s%s\n", grayFg, lightGray, app.WorkDir, reset)
	fmt.Printf("  %srouting   %schat %s│%s assist %s│%s swarm %s(auto)%s\n",
		grayFg, teal, grayFg, lavender, grayFg, orange, grayFg, reset)
	fmt.Printf("  %s%s%s\n", grayFg, strings.Repeat("─", 50), reset)
	fmt.Printf("  %s/session  /facts  /remember  /cron  /quit%s\n\n", grayFg, reset)

	registerTerminalHooks(app)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("  %s%s❯%s ", bold, peach, reset)
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/quit" || input == "/exit" {
			fmt.Printf("  %sgoodbye%s\n", grayFg, reset)
			break
		}

		if strings.HasPrefix(input, "/") {
			result := app.HandleCommand(input)
			if result != "" {
				fmt.Printf("\n  %s%s%s\n\n", grayFg, result, reset)
			}
			continue
		}

		app.Bus.Emit(event.Event{Type: event.UserMessage, Content: input})

		reply := app.HandleMessage(input)
		app.Bus.Emit(event.Event{Type: event.AssistantMessage, Content: reply})

		fmt.Println()
		for _, line := range strings.Split(reply, "\n") {
			fmt.Printf("  %s│%s %s\n", teal, reset, line)
		}
		fmt.Println()
	}
}

func roleColor(role string) string {
	switch {
	case strings.Contains(role, "bug") || strings.Contains(role, "error"):
		return red + bold + role
	case strings.Contains(role, "security") || strings.Contains(role, "audit"):
		return yellow + bold + role
	case strings.Contains(role, "test"):
		return green + bold + role
	case strings.Contains(role, "refactor") || strings.Contains(role, "complex"):
		return teal + bold + role
	case strings.Contains(role, "doc"):
		return blue + bold + role
	case strings.Contains(role, "optim") || strings.Contains(role, "perf"):
		return lavender + bold + role
	default:
		return peach + bold + role
	}
}
