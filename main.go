package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	markdown "github.com/MichaelMure/go-term-markdown"
)

const ctxTime = 2000

var browsers = []string{
	"google-chrome",
	"chrome",
	"chromium",
	"chromium-browser",
	"brave-browser",
	"edge",
}

func detectBrowser() (string, error) {
	for _, name := range browsers {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Chromium-based browser found in PATH")
}

func main() {
	usr, err := user.Current()
	if err != nil {
		fmt.Println("Error fetching user info:", err)
		return
	}

	configDir := usr.HomeDir + "/.config/chatbang"
	configPath := configDir + "/chatbang"
	profileDir := usr.HomeDir + "/.config/chatbang/profile_data"

	err = os.MkdirAll(configDir, 0o755)
	if err != nil {
		fmt.Println("Error creating config directory:", err)
		return
	}

	configFile, err := os.OpenFile(configPath,
		os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Println("Error opening config file:", err)
		return
	}
	defer configFile.Close()

	info, err := configFile.Stat()
	if err != nil {
		fmt.Println("Error getting file info:", err)
		return
	}

	if info.Size() == 0 {
		io.WriteString(configFile, "browser=\n")
		configFile.Seek(0, 0)
	}

	// read browser from config
	var defaultBrowser string
	scanner := bufio.NewScanner(configFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "browser" {
			defaultBrowser = strings.TrimSpace(parts[1])
		}
	}

	// Step 2: if config is empty or invalid, detect in PATH
	if defaultBrowser == "" || exec.Command(defaultBrowser).Run() != nil {
		detectedBrowser, err := detectBrowser()
		if err != nil {
			fmt.Println("No Chromium-based browser found in PATH or config.")
			fmt.Println("Please install a Chromium-based browser or edit the config at", configPath)
			return
		}
		defaultBrowser = detectedBrowser
	}

	if len(os.Args) > 1 {
		if os.Args[1] == "--config" {
			loginProfile(defaultBrowser, profileDir)
			return
		}

		if os.Args[1] == "--help" || os.Args[1] == "-h" {
			helpStr := "`Chatbang` is a simple tool to access ChatGPT from the terminal, without needing for an API key.  \n"

			helpStr += "## Configuration  \n `Chatbang` requires a Chromium-based browser (e.g. Chrome, Edge, Brave) to work, so you need to have one. And then make sure that it points to the right path to your chosen browser in the default config path for `Chatbang`: `$HOME/.config/chatbang/chatbang`.  \n\nIt's default is: ``` browser=/usr/bin/google-chrome ```  \nChange it to your favorite Chromium-based browser.  \n\n"

			helpStr += "You also need to log in to ChatGPT in `Chatbang`'s Chromium session, so you need to do: ```bash chatbang --config ``` That will open `Chatbang`'s Chromium session on ChatGPT's website, log in with your account. Then, you will need to allow the clipboard permission for ChatGPT's website (on the same session).  \n\n"

			res := markdown.Render(string(helpStr), 80, 2)
			fmt.Println(string(res))
			return
		}
	}

	fmt.Print("> ") // first prompt

	allocatorCtx, cancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(defaultBrowser),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
			chromedp.Flag("exclude-switches", "enable-automation"),
			chromedp.Flag("disable-extensions", false),
			chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
			chromedp.Flag("disable-default-apps", false),
			chromedp.Flag("disable-dev-shm-usage", false),
			chromedp.Flag("disable-gpu", false),
			//chromedp.Flag("headless", false),
			chromedp.UserDataDir(profileDir),
			chromedp.Flag("profile-directory", "Default"),
		)...,
	)

	defer cancel()

	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()

	taskCtx, taskCancel := context.WithTimeout(ctx, ctxTime*time.Second)
	defer taskCancel()

	err = chromedp.Run(taskCtx,
		chromedp.Navigate(`https://chatgpt.com`),
	)

	if err != nil {
		log.Fatal(err)
	}

	promptScanner := bufio.NewScanner(os.Stdin)

	for promptScanner.Scan() {
		firstPrompt := promptScanner.Text()
		if len(firstPrompt) > 0 {
			runChatGPT(taskCtx, defaultBrowser, profileDir, firstPrompt)
			return
		}

		fmt.Print("> ")
	}
}

func runChatGPT(taskCtx context.Context, browserPath string, profileDir string, firstPrompt string) {
	fmt.Printf("[Thinking...]\n\n")

	buttonDiv := `button[data-testid="copy-turn-action-button"]`

	modifiedPrompt := firstPrompt + " (Make an answer in less than 5 lines)."
	var copiedText string
	result := markdown.Render(string(modifiedPrompt), 80, 2)

	js := `new Promise((resolve, reject) => { window.navigator.clipboard.readText() .then(text => resolve(text)) .catch(err => reject(err)); });`

	err := chromedp.Run(taskCtx,
		chromedp.WaitVisible(`#prompt-textarea`, chromedp.ByID),
		chromedp.Click(`#prompt-textarea`, chromedp.ByID),
		chromedp.SendKeys(`#prompt-textarea`, modifiedPrompt, chromedp.ByID),
		chromedp.Click(`#composer-submit-button`, chromedp.ByID),
		chromedp.Click(`#prompt-textarea`, chromedp.ByID),
	)

	for {
		if copiedText != modifiedPrompt && len(copiedText) > 0 {
			break
		}
		// because it's sometimes doesn't see the very last copy button
		// so it copies the prompt instead
		err = chromedp.Run(taskCtx,
			//chromedp.Sleep(1*time.Second),
			chromedp.WaitVisible(buttonDiv, chromedp.ByQuery),

			chromedp.Evaluate(fmt.Sprintf(`
				(() => {
				    let buttons = document.querySelectorAll('%s');
				    if (buttons.length > 0) {
					buttons[buttons.length - 1].click();
				    }
				})()
			    `, buttonDiv), nil),

			// Read clipboard
			chromedp.Evaluate(js, &copiedText, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}),
		)

		result = markdown.Render(string(copiedText), 80, 2)
	}

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(result))

	fmt.Print("> ")
	promptScanner := bufio.NewScanner(os.Stdin)
	for promptScanner.Scan() {
		prompt := promptScanner.Text()
		modifiedPrompt = prompt + " (Make an answer in less than 5 lines)."
		if len(prompt) == 0 {
			fmt.Print("> ")
			continue
		}

		fmt.Printf("[Thinking...]\n\n")

		err := chromedp.Run(taskCtx,
			chromedp.WaitVisible(`#prompt-textarea`, chromedp.ByID),
			chromedp.Click(`#prompt-textarea`, chromedp.ByID),
			chromedp.SendKeys(`#prompt-textarea`, modifiedPrompt, chromedp.ByID),
			chromedp.Click(`#composer-submit-button`, chromedp.ByID),
			chromedp.Click(`#prompt-textarea`, chromedp.ByID),
		)

		if err != nil {
			log.Fatal(err)
		}

		result = markdown.Render(string(copiedText), 80, 2)

		copiedText = ""

		for {
			if copiedText != modifiedPrompt && len(copiedText) > 0 {
				break
			}
			// because it's sometimes doesn't see the very last copy button
			// so it copies the prompt instead

			err = chromedp.Run(taskCtx,
				chromedp.Sleep(3*time.Second),
				chromedp.Evaluate(fmt.Sprintf(`
					(() => {
					    let buttons = document.querySelectorAll('%s');
					    if (buttons.length > 0) {
						buttons[buttons.length - 1].click();
					    }
					})()
				    `, buttonDiv), nil),

				chromedp.Sleep(1*time.Second),
				// Read clipboard
				chromedp.Evaluate(js, &copiedText, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
					return p.WithAwaitPromise(true)
				}),
			)

			result = markdown.Render(string(copiedText), 80, 2)
		}

		fmt.Println(string(result))

		fmt.Print("> ")
	}
}

func loginProfile(defaultBrowser string, profileDir string) {
	browserPath := defaultBrowser

	allocatorCtx, cancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(browserPath),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
			chromedp.Flag("exclude-switches", "enable-automation"),
			chromedp.Flag("disable-extensions", false),
			chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
			chromedp.Flag("disable-default-apps", false),
			chromedp.Flag("disable-dev-shm-usage", false),
			chromedp.Flag("disable-gpu", false),
			chromedp.Flag("headless", false),
			chromedp.UserDataDir(profileDir),
			chromedp.Flag("profile-directory", "Default"),
		)...,
	)

	defer cancel()

	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()

	taskCtx, taskCancel := context.WithTimeout(ctx, ctxTime*time.Second)
	defer taskCancel()

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(`https://www.chatgpt.com/`),
		chromedp.Evaluate(`(async () => {
			const permName = 'clipboard-read';
			try {
				const p = await navigator.permissions.query({ name: permName });
				if (p.state !== 'granted') {
					alert("Please allow clipboard access in the popup that will appear now.");
				}
			} catch (e) {
				try {
					await navigator.clipboard.readText();
				} catch (_) {
					alert("Please allow clipboard access in the popup that will appear now.");
				}
			}
		})();`, nil),
		chromedp.Evaluate(`navigator.clipboard.readText().catch(() => {});`, nil),
	)

	if err != nil {
		log.Fatal(err)
	}

	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(ctxTime * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState`, nil))
				if err != nil {
					done <- true
					return
				}
			case <-ctx.Done():
				done <- true
				return
			}
		}
	}()

	<-done
}
