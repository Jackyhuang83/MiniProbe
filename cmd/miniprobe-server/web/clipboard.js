"use strict";
(() => {
  const $ = id => document.getElementById(id);
  const base = "/api/v1/subscriptions";
  let csrf = "";
  let poll = null;
  let busy = false;

  $("clipMenu").addEventListener("click", () => {
    const open = $("clipNav").classList.toggle("open");
    $("clipMenu").setAttribute("aria-expanded", String(open));
  });

  function notice(message, error = false) {
    const el = $("clipNotice");
    el.textContent = message;
    el.classList.toggle("error", error);
    el.classList.remove("hidden");
  }
  function showWorkspace(ready) {
    $("clipGate").classList.toggle("hidden", ready);
    $("clipWorkspace").classList.toggle("hidden", !ready);
    $("clipLogout").classList.toggle("hidden", !ready);
  }
  async function request(path, options = {}, write = false) {
    if (write && !csrf) throw new Error("安全令牌不可用，请重新登录。");
    const headers = {"X-MiniProbe-Request":"1","X-Requested-With":"XMLHttpRequest",
      ...(write ? {"X-MiniProbe-Subscription-CSRF":csrf} : {}),
      ...(options.body !== undefined ? {"Content-Type":"application/json"} : {})};
    const response = await fetch(base + path, {
      credentials:"same-origin", cache:"no-store", ...options, headers:{...headers,...(options.headers||{})}
    });
    if (!response.ok) {
      const msg = {400:"内容无效或超过 16 KiB",401:"登录已过期，请重新登录",403:"HTTPS 或安全校验失败，请刷新页面",404:"记录不存在，请刷新",405:"不支持的请求方式",429:"操作过于频繁"}[response.status];
      if (response.status === 401) { csrf = ""; showWorkspace(false); stopPolling(); }
      throw new Error(msg || "请求失败（HTTP " + response.status + "）");
    }
    return response.json();
  }
  function stopPolling() {
    if (poll !== null) { clearInterval(poll); poll = null; }
  }
  function displayTime(raw) {
    const date = new Date(raw);
    return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString("zh-CN", {month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit"});
  }
  function makeButton(title, color, callback) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "clip-button " + (color || "secondary");
    button.textContent = title;
    button.addEventListener("click", callback);
    return button;
  }
  async function copyText(text) {
    try {
      if (!navigator.clipboard || !navigator.clipboard.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(text);
      notice("已复制到设备剪贴板。");
    } catch (_) {
      // Never overwrite an unsaved draft when clipboard permissions are denied.
      const fallback = document.createElement("textarea");
      fallback.value = text;
      fallback.style.position = "fixed";
      fallback.style.left = "-9999px";
      document.body.append(fallback);
      fallback.focus();
      fallback.select();
      let ok = false;
      try { ok = document.execCommand("copy"); } catch (_) {}
      fallback.remove();
      notice(ok ? "已复制到设备剪贴板。" : "浏览器未允许自动复制，请长按或选择记录中的文字手动复制。", !ok);
    }
  }
  function render(items) {
    const root = $("clipItems");
    root.replaceChildren();
    $("clipCount").textContent = "（" + items.length + " / 50）";
    $("clipEmpty").classList.toggle("hidden", items.length !== 0);
    $("clipClear").disabled = items.length === 0;
    for (const item of items) {
      const article = document.createElement("article");
      article.className = "clip-item";
      const content = document.createElement("p");
      content.className = "clip-item-text";
      content.textContent = item.text;
      const footer = document.createElement("div");
      footer.className = "clip-item-footer";
      const meta = document.createElement("span");
      meta.className = "clip-item-time";
      meta.textContent = "保存：" + displayTime(item.created_at) + " · 过期：" + displayTime(item.expires_at);
      const actions = document.createElement("div");
      actions.className = "clip-item-buttons";
      actions.append(makeButton("复制", "secondary", () => copyText(item.text)));
      actions.append(makeButton("删除", "danger", async () => {
        if (!window.confirm("确定删除这条内容？删除后将无法从当前剪贴板恢复。")) return;
        try { await request("/clipboard?id=" + encodeURIComponent(item.id), {method:"DELETE"}, true); await loadItems(); notice("已删除。"); }
        catch(e) { notice(e.message, true); }
      }));
      footer.append(meta, actions);
      article.append(content, footer);
      root.append(article);
    }
  }
  async function loadItems() {
    const data = await request("/clipboard");
    render(Array.isArray(data.items) ? data.items : []);
  }
  async function loadSession() {
    const session = await request("/session");
    csrf = session.authenticated ? (session.csrf || "") : "";
    showWorkspace(Boolean(session.authenticated && csrf));
    if (session.authenticated && csrf) {
      await loadItems();
      stopPolling();
      poll = setInterval(() => { if(!busy) loadItems().catch(e => notice(e.message,true)); }, 12000);
    } else {
      stopPolling();
      if(!session.secure) notice("剪贴板仅支持 HTTPS，请改用 MiniProbe HTTPS 域名访问。",true);
      else if(!session.enabled) notice("请先通过 MiniProbe SSH 管理菜单 15 → 6 设置订阅中心独立密码。",true);
    }
  }

  $("clipLoginForm").addEventListener("submit", async event => {
    event.preventDefault();
    if (busy) return;
    busy = true;
    try {
      await request("/login", {method:"POST",body:JSON.stringify({password:$("clipPassword").value})});
      $("clipPassword").value = "";
      $("clipNotice").classList.add("hidden");
      await loadSession();
    } catch(e) { notice(e.message,true); }
    finally { busy = false; }
  });
  $("clipAddForm").addEventListener("submit", async event => {
    event.preventDefault();
    if (busy) return;
    const text = $("clipText").value;
    if (!text.trim()) return notice("请先粘贴内容。", true);
    if (new TextEncoder().encode(text).length > 16384) return notice("文本超过 16 KiB，请分段保存。",true);
    busy = true;
    $("clipAdd").disabled = true;
    try {
      await request("/clipboard",{method:"POST",body:JSON.stringify({text})},true);
      $("clipText").value = "";
      await loadItems();
      notice("已保存，其他设备刷新页面即可复制。");
    } catch(e) { notice(e.message,true); }
    finally {busy=false;$("clipAdd").disabled=false;}
  });
  $("clipClear").addEventListener("click",async () => {
    if (busy || !window.confirm("确定清空全部剪贴板内容？该操作无法从当前存储恢复。")) return;
    busy = true;
    try { await request("/clipboard?all=1",{method:"DELETE"},true); await loadItems(); notice("全部内容已清空。"); }
    catch(e) { notice(e.message,true); }
    finally { busy=false; }
  });
  $("clipLogout").addEventListener("click",async () => {
    try {
      await request("/logout",{method:"POST"});
      csrf = ""; stopPolling(); render([]); showWorkspace(false);
      notice("已退出登录。");
    } catch(e) { notice(e.message,true); }
  });
  document.addEventListener("visibilitychange",() => {
    if (!document.hidden && csrf && !busy) loadItems().catch(e=>notice(e.message,true));
  });
  loadSession().catch(e=>notice(e.message,true));
})();