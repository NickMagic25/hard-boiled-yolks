const state = {
  cwd: "/",
  selected: null,
  encoding: "utf-8",
  ws: null,
  entries: [],
  selecting: false,
  selectedPaths: new Set(),
};

const $ = (id) => document.getElementById(id);
const osTheme = window.matchMedia
  ? window.matchMedia("(prefers-color-scheme: dark)")
  : { matches: false };

function normalizeThemeMode(mode) {
  return mode === "light" || mode === "dark" || mode === "system" ? mode : "system";
}

function storedThemeMode() {
  try {
    return normalizeThemeMode(
      localStorage.getItem("hby-control-theme-mode") ||
        localStorage.getItem("hby-control-theme") ||
        "system",
    );
  } catch {
    return "system";
  }
}

function resolvedTheme(mode) {
  return mode === "system" ? (osTheme.matches ? "dark" : "light") : mode;
}

function applyThemeMode(mode) {
  mode = normalizeThemeMode(mode);
  document.documentElement.dataset.themeMode = mode;
  document.documentElement.dataset.theme = resolvedTheme(mode);
  document.querySelectorAll('input[name="themeMode"]').forEach((input) => {
    input.checked = input.value === mode;
  });
}

function saveThemeMode(mode) {
  mode = normalizeThemeMode(mode);
  try {
    localStorage.setItem("hby-control-theme-mode", mode);
    localStorage.removeItem("hby-control-theme");
  } catch {}
  applyThemeMode(mode);
}

function toast(msg) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.remove("hide");
  setTimeout(() => t.classList.add("hide"), 2600);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
    ...opts,
  });
  if (res.status === 401) {
    location.href = "/login";
    return;
  }
  const txt = await res.text();
  let data = {};
  try {
    data = txt ? JSON.parse(txt) : {};
  } catch {}
  if (!res.ok) {
    throw new Error(data.error || res.statusText);
  }
  return data;
}

function fmtSize(n) {
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
  return (n / 1048576).toFixed(1) + " MB";
}

function parentPath(p) {
  if (p === "/") return "/";
  const parts = p.split("/").filter(Boolean);
  parts.pop();
  return "/" + parts.join("/");
}

function joinPath(dir, name) {
  return (dir === "/" ? "/" : dir + "/") + name.replace(/^\/+/, "");
}

function selectedList() {
  return Array.from(state.selectedPaths);
}

function selectedEntries() {
  const wanted = new Set(selectedList());
  return state.entries.filter((ent) => wanted.has(ent.path));
}

function isTarPath(p) {
  p = p.toLowerCase();
  return p.endsWith(".tar") || p.endsWith(".tar.gz") || p.endsWith(".tgz");
}

function closeRowMenu() {
  $("rowMenu").classList.add("hide");
  $("rowMenu").innerHTML = "";
}

async function loadStatus() {
  const s = await api("/api/status");
  const p = s.process;
  $("statusDot").classList.toggle("running", p.running);
  $("statusText").textContent = p.running ? "Running" : "Stopped";
  $("startBtn").disabled = p.running || !p.configured;
  $("stopBtn").disabled = !p.running;
  $("restartBtn").disabled = !p.configured;
}

function updateSelectionBar() {
  const count = state.selectedPaths.size;
  $("selectionBar").hidden = !state.selecting;
  $("selectionCount").textContent = count + " selected";
  $("moveSelectedBtn").disabled = count === 0;
  $("tarSelectedBtn").disabled = count === 0;
  $("deleteSelectedBtn").disabled = count === 0;

  const entries = selectedEntries();
  $("untarSelectedBtn").disabled = !(
    entries.length === 1 &&
    !entries[0].isDir &&
    isTarPath(entries[0].path)
  );
}

function setSelecting(on) {
  state.selecting = on;
  if (!on) state.selectedPaths.clear();
  closeRowMenu();
  updateSelectionBar();
}

function toggleSelection(path) {
  if (state.selectedPaths.has(path)) {
    state.selectedPaths.delete(path);
  } else {
    state.selectedPaths.add(path);
  }
  updateSelectionBar();
  loadFiles(state.cwd).catch((e) => toast(e.message));
}

function enterSelection(path) {
  state.selecting = true;
  state.selectedPaths.add(path);
  closeRowMenu();
  updateSelectionBar();
  loadFiles(state.cwd).catch((e) => toast(e.message));
}

function openRowMenu(ent, button) {
  const menu = $("rowMenu");
  menu.innerHTML = "";

  const select = document.createElement("button");
  select.textContent = "Select";
  select.onclick = () => enterSelection(ent.path);
  menu.appendChild(select);

  if (!ent.isDir) {
    const open = document.createElement("button");
    open.textContent = "Open";
    open.onclick = () => {
      closeRowMenu();
      openFile(ent.path).catch((e) => toast(e.message));
    };
    menu.appendChild(open);

    const download = document.createElement("a");
    download.textContent = "Download";
    download.href = "/api/fs/download?path=" + encodeURIComponent(ent.path);
    download.onclick = () => closeRowMenu();
    menu.appendChild(download);

    if (isTarPath(ent.path)) {
      const untar = document.createElement("button");
      untar.textContent = "Untar";
      untar.onclick = () => {
        closeRowMenu();
        extractArchive(ent.path).catch((e) => toast(e.message));
      };
      menu.appendChild(untar);
    }
  }

  const del = document.createElement("button");
  del.textContent = "Delete";
  del.className = "danger";
  del.onclick = () => {
    closeRowMenu();
    deletePaths([ent.path]).catch((e) => toast(e.message));
  };
  menu.appendChild(del);

  const rect = button.getBoundingClientRect();
  menu.style.left = Math.min(rect.left, innerWidth - 150) + "px";
  menu.style.top = Math.min(rect.bottom + 4, innerHeight - 160) + "px";
  menu.classList.remove("hide");
}

function renderEntry(ent) {
  const row = document.createElement("button");
  row.className =
    "entry" +
    (state.selected === ent.path ? " active" : "") +
    (state.selectedPaths.has(ent.path) ? " selected" : "");
  row.innerHTML =
    "<span>" +
    (ent.isDir ? "▸" : "•") +
    '</span><span class="name"></span><span class="size">' +
    (ent.isDir ? "" : fmtSize(ent.size)) +
    '</span><span class="rowSlot"></span>';
  row.querySelector(".name").textContent = ent.name;

  const slot = row.querySelector(".rowSlot");
  if (state.selecting) {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.className = "selectBox";
    box.checked = state.selectedPaths.has(ent.path);
    box.onclick = (e) => {
      e.stopPropagation();
      toggleSelection(ent.path);
    };
    slot.appendChild(box);
  } else {
    const action = document.createElement("button");
    action.type = "button";
    action.className = "rowAction";
    action.title = "Actions";
    action.textContent = "⋯";
    action.onclick = (e) => {
      e.stopPropagation();
      openRowMenu(ent, action);
    };
    slot.appendChild(action);
  }

  row.onclick = () => {
    if (state.selecting) {
      toggleSelection(ent.path);
      return;
    }
    ent.isDir ? loadFiles(ent.path) : openFile(ent.path);
  };
  return row;
}

async function loadFiles(p = state.cwd) {
  const data = await api("/api/fs/list?path=" + encodeURIComponent(p));
  state.cwd = data.path;
  state.entries = data.entries || [];
  state.selectedPaths.forEach((path) => {
    if (!state.entries.some((ent) => ent.path === path)) {
      state.selectedPaths.delete(path);
    }
  });

  $("cwd").textContent = data.path;
  $("upBtn").disabled = data.path === "/";
  const list = $("fileList");
  list.innerHTML = "";
  for (const ent of state.entries) {
    list.appendChild(renderEntry(ent));
  }
  updateSelectionBar();
}

async function openFile(p) {
  show("editor");
  const data = await api("/api/fs/read?path=" + encodeURIComponent(p));
  state.selected = data.path;
  state.encoding = data.encoding;
  $("filename").textContent = data.path;
  $("downloadBtn").href = "/api/fs/download?path=" + encodeURIComponent(data.path);
  $("editor").disabled = data.encoding !== "utf-8";
  $("editor").value = data.encoding === "utf-8" ? data.content : "Binary file. Use Download to inspect it.";
  $("emptyState").classList.add("hide");
  $("editor").classList.remove("hide");
  await loadFiles(state.cwd);
}

async function saveFile() {
  if (!state.selected) return;
  await api("/api/fs/write", {
    method: "POST",
    body: JSON.stringify({
      path: state.selected,
      content: $("editor").value,
      encoding: state.encoding,
    }),
  });
  toast("Saved");
  await loadFiles(state.cwd);
}

async function deleteCurrentFile() {
  if (!state.selected) return;
  if (!confirm("Delete " + state.selected + "?")) return;
  const deleted = await deletePaths([state.selected], false);
  if (!deleted) return;
  state.selected = null;
  $("editor").value = "";
  $("editor").disabled = true;
  $("filename").textContent = "No file selected";
  show("files");
}

async function deletePaths(paths, confirmFirst = true) {
  if (paths.length === 0) return false;
  if (confirmFirst && !confirm("Delete " + paths.length + " selected item" + (paths.length === 1 ? "" : "s") + "?")) {
    return false;
  }
  await api("/api/fs/delete", {
    method: "POST",
    body: JSON.stringify({ paths }),
  });
  paths.forEach((path) => state.selectedPaths.delete(path));
  if (state.selected && paths.includes(state.selected)) state.selected = null;
  toast("Deleted");
  if (state.selectedPaths.size === 0) setSelecting(false);
  await loadFiles(state.cwd);
  return true;
}

async function moveSelected() {
  const paths = selectedList();
  if (paths.length === 0) return;
  const dest = prompt("Move to folder", state.cwd);
  if (!dest) return;
  await api("/api/fs/move", {
    method: "POST",
    body: JSON.stringify({ paths, destination: dest }),
  });
  toast("Moved");
  setSelecting(false);
  await loadFiles(state.cwd);
}

async function tarSelected() {
  const paths = selectedList();
  if (paths.length === 0) return;
  const name = prompt("Archive path", joinPath(state.cwd, "archive.tar"));
  if (!name) return;
  await api("/api/fs/archive", {
    method: "POST",
    body: JSON.stringify({ paths, destination: name }),
  });
  toast("Archive created");
  setSelecting(false);
  await loadFiles(state.cwd);
}

async function extractArchive(path) {
  const dest = prompt("Extract to folder", state.cwd);
  if (!dest) return;
  await api("/api/fs/extract", {
    method: "POST",
    body: JSON.stringify({ path, destination: dest }),
  });
  toast("Extracted");
  setSelecting(false);
  await loadFiles(state.cwd);
}

async function untarSelected() {
  const entries = selectedEntries();
  if (entries.length !== 1) return;
  await extractArchive(entries[0].path);
}

async function newFile() {
  const name = prompt("File name");
  if (!name) return;
  const p = (state.cwd === "/" ? "/" : state.cwd + "/") + name;
  await api("/api/fs/write", {
    method: "POST",
    body: JSON.stringify({ path: p, content: "", encoding: "utf-8" }),
  });
  await loadFiles(state.cwd);
  await openFile(p);
}

async function newDir() {
  const name = prompt("Folder name");
  if (!name) return;
  await api("/api/fs/mkdir", {
    method: "POST",
    body: JSON.stringify({ path: (state.cwd === "/" ? "/" : state.cwd + "/") + name }),
  });
  await loadFiles(state.cwd);
}

async function uploadFiles(files) {
  const fd = new FormData();
  for (const f of files) {
    fd.append("file", f);
  }
  const res = await fetch("/api/fs/upload?path=" + encodeURIComponent(state.cwd), {
    method: "POST",
    body: fd,
  });
  if (!res.ok) {
    const d = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(d.error);
  }
  toast("Uploaded");
  await loadFiles(state.cwd);
}

function scrollConsoleToBottom() {
  const out = $("consoleOutput");
  requestAnimationFrame(() => {
    out.scrollTop = out.scrollHeight;
  });
}

function clearConsole() {
  const out = $("consoleOutput");
  out.textContent = "";
  scrollConsoleToBottom();
}

function appendConsole(data) {
  const out = $("consoleOutput");
  out.textContent += data;
  scrollConsoleToBottom();
}

async function proc(action) {
  clearConsole();
  await api("/api/process/" + action, { method: "POST", body: "{}" });
  await loadStatus();
}

function connectConsole() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(proto + "://" + location.host + "/api/console/ws");
  state.ws = ws;
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === "clear") clearConsole();
    if (msg.type === "output" || msg.type === "error") appendConsole(msg.data || "");
  };
  ws.onclose = () => setTimeout(connectConsole, 1500);
}

function show(which) {
  closeRowMenu();
  const filesOn = which === "files";
  const consoleOn = which === "console";
  $("filesPage").hidden = !filesOn;
  $("workspacePage").hidden = filesOn;
  $("consolePane").style.display = consoleOn ? "grid" : "none";
  $("editorPane").style.display = !filesOn && !consoleOn ? "grid" : "none";
  $("consoleTab").classList.toggle("active", consoleOn);
  $("editorTab").classList.toggle("active", !consoleOn);
  if (consoleOn) scrollConsoleToBottom();
}

applyThemeMode(storedThemeMode());
document.querySelectorAll('input[name="themeMode"]').forEach((input) => {
  input.onchange = (e) => {
    if (e.target.checked) saveThemeMode(e.target.value);
  };
});
if (osTheme.addEventListener) {
  osTheme.addEventListener("change", () => {
    if (document.documentElement.dataset.themeMode === "system") applyThemeMode("system");
  });
} else if (osTheme.addListener) {
  osTheme.addListener(() => {
    if (document.documentElement.dataset.themeMode === "system") applyThemeMode("system");
  });
}

$("upBtn").onclick = () => loadFiles(parentPath(state.cwd));
$("refreshBtn").onclick = () => loadFiles(state.cwd);
$("newFileBtn").onclick = () => newFile().catch((e) => toast(e.message));
$("newDirBtn").onclick = () => newDir().catch((e) => toast(e.message));
$("saveBtn").onclick = () => saveFile().catch((e) => toast(e.message));
$("deleteBtn").onclick = () => deleteCurrentFile().catch((e) => toast(e.message));
$("uploadBtn").onclick = () => $("uploadInput").click();
$("uploadInput").onchange = (e) => uploadFiles(e.target.files).catch((err) => toast(err.message));
$("moveSelectedBtn").onclick = () => moveSelected().catch((e) => toast(e.message));
$("tarSelectedBtn").onclick = () => tarSelected().catch((e) => toast(e.message));
$("untarSelectedBtn").onclick = () => untarSelected().catch((e) => toast(e.message));
$("deleteSelectedBtn").onclick = () => deletePaths(selectedList()).catch((e) => toast(e.message));
$("clearSelectionBtn").onclick = () => setSelecting(false);
$("backToFilesBtn").onclick = () => show("files");
$("startBtn").onclick = () => proc("start").catch((e) => toast(e.message));
$("stopBtn").onclick = () => proc("stop").catch((e) => toast(e.message));
$("restartBtn").onclick = () => proc("restart").catch((e) => toast(e.message));
$("consoleForm").onsubmit = (e) => {
  e.preventDefault();
  const input = $("consoleInput");
  if (input.value && state.ws && state.ws.readyState === 1) {
    state.ws.send(JSON.stringify({ type: "input", data: input.value + "\n" }));
    input.value = "";
  }
};
$("editorTab").onclick = () => show("files");
$("consoleTab").onclick = () => show("console");
document.addEventListener("click", (e) => {
  if (!$("rowMenu").contains(e.target)) closeRowMenu();
});

show("files");
loadFiles("/").catch((e) => toast(e.message));
loadStatus().catch((e) => toast(e.message));
setInterval(() => loadStatus().catch(() => {}), 3000);
connectConsole();
