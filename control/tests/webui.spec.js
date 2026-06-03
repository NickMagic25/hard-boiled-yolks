const { test, expect } = require("@playwright/test");
const childProcess = require("node:child_process");
const fs = require("node:fs/promises");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");

const controlDir = path.resolve(__dirname, "..");

let binaryPath;
let binaryTempDir;
let goCacheDir;

test.beforeAll(async () => {
  binaryTempDir = await fs.mkdtemp(path.join(os.tmpdir(), "hby-control-bin-"));
  goCacheDir = await fs.mkdtemp(path.join(os.tmpdir(), "hby-control-gocache-"));
  binaryPath = path.join(binaryTempDir, process.platform === "win32" ? "hby-control.exe" : "hby-control");

  childProcess.execFileSync("go", ["build", "-o", binaryPath, "./cmd/hby-control"], {
    cwd: controlDir,
    env: { ...process.env, GOCACHE: process.env.GOCACHE || goCacheDir },
    stdio: "inherit",
  });
});

test.afterAll(async () => {
  await removeIfExists(binaryTempDir);
  await removeIfExists(goCacheDir);
});

test("supports browsing, editing, creating, and deleting files", async ({ page }) => {
  await usingControlServer(async ({ baseURL, root }) => {
    await page.goto(baseURL);

    await expect(page.getByRole("heading", { name: "Server Control" })).toBeVisible();
    await expect(page.locator("#statusText")).toHaveText("Running");
    await expect(page.locator("#fileList")).toContainText("server.properties");

    await page.locator("#fileList .entry").filter({ hasText: "server.properties" }).click();
    await expect(page.locator("#filename")).toHaveText("/server.properties");
    await expect(page.locator("#editor")).toHaveValue(/motd=Hard Boiled Yolks/);

    await page.locator("#editor").fill("motd=Changed from Playwright\nmax-players=8\n");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#toast")).toHaveText("Saved");
    await expectFile(root, "server.properties", "motd=Changed from Playwright\nmax-players=8\n");

    page.once("dialog", dialog => dialog.accept("created.txt"));
    await page.getByRole("button", { name: "File" }).click();
    await page.locator("#fileList .entry").filter({ hasText: "created.txt" }).click();
    await expect(page.locator("#filename")).toHaveText("/created.txt");
    await expect(page.locator("#editor")).toHaveValue("");
    await expectFile(root, "created.txt", "");

    page.once("dialog", dialog => dialog.accept());
    await page.getByRole("button", { name: "Delete" }).click();
    await expect(page.locator("#fileList")).not.toContainText("created.txt");
    await expectMissing(root, "created.txt");
  });
});

test("uploads files through the web UI", async ({ page }) => {
  await usingControlServer(async ({ baseURL, root, tempDir }) => {
    const uploadPath = path.join(tempDir, "uploaded-config.yml");
    await fs.writeFile(uploadPath, "difficulty: hard\n", "utf8");

    await page.goto(baseURL);
    await page.setInputFiles("#uploadInput", uploadPath);

    await expect(page.locator("#toast")).toHaveText("Uploaded");
    await expect(page.locator("#fileList")).toContainText("uploaded-config.yml");
    await expectFile(root, "uploaded-config.yml", "difficulty: hard\n");
  });
});

test("switches theme and uses sidebar view tabs", async ({ page }) => {
  await usingControlServer(async ({ baseURL }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto(baseURL);

    await expect(page.locator("#editorTab")).toBeVisible();
    await expect(page.locator("#consoleTab")).toBeVisible();
    await expect(page.locator("#editorPane")).toBeVisible();
    await expect(page.locator("#consolePane")).toBeHidden();
    await expect(page.locator("html")).toHaveAttribute("data-theme-mode", "system");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator("#themeSystem")).toBeChecked();

    await page.emulateMedia({ colorScheme: "light" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

    await page.locator("#themeDark").check({ force: true });
    await expect(page.locator("html")).toHaveAttribute("data-theme-mode", "dark");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect.poll(() => page.evaluate(() => localStorage.getItem("hby-control-theme-mode"))).toBe("dark");

    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme-mode", "dark");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator("#themeDark")).toBeChecked();

    await page.locator("#themeSystem").check({ force: true });
    await expect(page.locator("html")).toHaveAttribute("data-theme-mode", "system");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

    await page.locator("#consoleTab").click();
    await expect(page.locator("#consolePane")).toBeVisible();
    await expect(page.locator("#editorPane")).toBeHidden();
  });
});

test("streams console input and controls the supervised process", async ({ page }) => {
  await usingControlServer(async ({ baseURL }) => {
    await page.goto(baseURL);
    await page.getByRole("button", { name: "Console" }).click();

    await expect(page.locator("#consoleOutput")).toContainText("[fake-server] ready");

    await page.locator("#consoleInput").fill("say hello");
    await page.getByRole("button", { name: "Send" }).click();
    await expect(page.locator("#consoleOutput")).toContainText("[fake-server] command:say hello");

    await page.getByRole("button", { name: "Stop" }).click();
    await expect(page.locator("#statusText")).toHaveText("Stopped");
    await expect(page.locator("#startBtn")).toBeEnabled();

    await page.locator("#startBtn").click();
    await expect(page.locator("#statusText")).toHaveText("Running");

    await page.getByRole("button", { name: "Restart" }).click();
    await expect(page.locator("#statusText")).toHaveText("Running");

    await page.locator("#consoleInput").fill("list");
    await page.getByRole("button", { name: "Send" }).click();
    await expect(page.locator("#consoleOutput")).toContainText("[fake-server] command:list");
  });
});

test("requires password login when credentials are configured", async ({ page, request }) => {
  await usingControlServer({ username: "admin", password: "secret", autoStart: false }, async ({ baseURL }) => {
    const apiResponse = await request.get(`${baseURL}/api/status`);
    expect(apiResponse.status()).toBe(401);

    await page.goto(baseURL);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByLabel("Username")).toBeVisible();

    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password").fill("secret");
    await page.getByRole("button", { name: "Log In" }).click();

    await expect(page).toHaveURL(`${baseURL}/`);
    await expect(page.getByRole("heading", { name: "Server Control" })).toBeVisible();
    await expect(page.locator("#statusText")).toHaveText("Stopped");
  });
});

async function usingControlServer(optionsOrCallback, maybeCallback) {
  const options = typeof optionsOrCallback === "function" ? {} : optionsOrCallback;
  const callback = typeof optionsOrCallback === "function" ? optionsOrCallback : maybeCallback;
  const server = await startControlServer(options);

  try {
    await callback(server);
  } finally {
    await server.stop();
  }
}

async function startControlServer(options = {}) {
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), "hby-control-webui-"));
  const root = path.join(tempDir, "root");
  await fs.mkdir(root, { recursive: true });
  await fs.writeFile(path.join(root, "server.properties"), "motd=Hard Boiled Yolks\nmax-players=20\n", "utf8");

  const serverScript = path.join(root, "server.sh");
  await fs.writeFile(serverScript, [
    "#!/bin/sh",
    "echo \"[fake-server] ready\"",
    "while IFS= read -r line; do",
    "  echo \"[fake-server] command:$line\"",
    "  if [ \"$line\" = \"stop\" ]; then",
    "    echo \"[fake-server] stopping\"",
    "    exit 0",
    "  fi",
    "done",
    "",
  ].join("\n"), "utf8");
  await fs.chmod(serverScript, 0o755);

  const port = await getFreePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const env = {
    ...process.env,
    HBY_CONTROL_ADDR: `127.0.0.1:${port}`,
    HBY_CONTROL_ROOT: root,
    HBY_CONTROL_STOP_TIMEOUT: "500ms",
    HBY_CONTROL_SESSION_KEY: "playwright-test-session-key",
    HBY_CONTROL_AUTO_START: String(options.autoStart ?? true),
  };

  if (options.username && options.password) {
    env.HBY_CONTROL_USERNAME = options.username;
    env.HBY_CONTROL_PASSWORD = options.password;
  }

  const child = childProcess.spawn(binaryPath, ["run", "--", "./server.sh"], {
    cwd: controlDir,
    env,
    detached: process.platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });

  let output = "";
  child.stdout.on("data", chunk => {
    output += chunk.toString();
  });
  child.stderr.on("data", chunk => {
    output += chunk.toString();
  });

  await waitForHTTP(baseURL, () => output);

  return {
    baseURL,
    root,
    tempDir,
    async stop() {
      await stopProcess(child);
      await removeIfExists(tempDir);
    },
  };
}

async function waitForHTTP(baseURL, getOutput) {
  const deadline = Date.now() + 10_000;
  let lastError;

  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/api/status`);
      if (response.status < 500) {
        return;
      }
      lastError = new Error(`HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await delay(100);
  }

  throw new Error(`control server did not start: ${lastError?.message || "timeout"}\n${getOutput()}`);
}

async function getFreePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

async function stopProcess(child) {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }

  await new Promise(resolve => {
    child.once("exit", resolve);
    if (process.platform === "win32") {
      child.kill("SIGTERM");
    } else {
      process.kill(-child.pid, "SIGTERM");
    }
    setTimeout(() => {
      if (child.exitCode === null && child.signalCode === null) {
        if (process.platform === "win32") {
          child.kill("SIGKILL");
        } else {
          process.kill(-child.pid, "SIGKILL");
        }
      }
    }, 2_000).unref();
  });
}

async function expectFile(root, name, expected) {
  await expect.poll(async () => fs.readFile(path.join(root, name), "utf8")).toBe(expected);
}

async function expectMissing(root, name) {
  await expect.poll(async () => {
    try {
      await fs.access(path.join(root, name));
      return false;
    } catch {
      return true;
    }
  }).toBe(true);
}

async function removeIfExists(target) {
  if (!target) {
    return;
  }
  await fs.rm(target, { recursive: true, force: true });
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}
