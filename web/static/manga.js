(() => {
  "use strict";
  const body = document.body;
  const provider = body.dataset.provider;
  const album = body.dataset.album;
  const reader = document.querySelector("#reader");
  const drawer = document.querySelector("#chapters");
  const nav = drawer.querySelector("nav");
  const resume = document.querySelector("#resume");
  const storageKey = `image-gateway:manga:v1:${provider}:${album}`;
  const retryDelays = [1000, 2500, 5000, 10000];
  const failedPages = new Set();
  let saved;
  try { saved = JSON.parse(localStorage.getItem(storageKey) || "null"); } catch {}

  document.querySelector("#chapters-button").addEventListener("click", () => {
    drawer.hidden = !drawer.hidden;
  });

  function loadImage(img, manual = false) {
    const box = img.closest(".page");
    clearTimeout(img.retryTimer);
    box.classList.remove("page-error");
    box.removeAttribute("role");
    box.removeAttribute("tabindex");
    box.onclick = null;
    img.hidden = false;
    if (manual) img.retryCount = 0;
    const separator = img.dataset.url.includes("?") ? "&" : "?";
    img.src = `${img.dataset.url}${separator}attempt=${Date.now()}`;
  }

  function imageFailed(img) {
    const box = img.closest(".page");
    img.retryCount = (img.retryCount || 0) + 1;
    failedPages.add(img);
    if (img.retryCount <= retryDelays.length) {
      box.classList.add("page-error");
      box.setAttribute("aria-label", `图片加载失败，正在进行第 ${img.retryCount} 次重试`);
      img.retryTimer = setTimeout(() => loadImage(img), retryDelays[img.retryCount - 1]);
      return;
    }
    img.hidden = true;
    box.classList.add("page-error");
    box.setAttribute("role", "button");
    box.setAttribute("tabindex", "0");
    box.setAttribute("aria-label", "图片加载失败，点击重新加载");
    box.onclick = () => loadImage(img, true);
    box.onkeydown = (event) => {
      if (event.key === "Enter" || event.key === " ") loadImage(img, true);
    };
  }

  window.addEventListener("online", () => {
    for (const img of failedPages) loadImage(img, true);
  });

  fetch(`/api/manga/${encodeURIComponent(provider)}/${encodeURIComponent(album)}`)
    .then((response) => {
      if (!response.ok) throw new Error();
      return response.json();
    })
    .then((manifest) => {
      document.title = manifest.title;
      document.querySelector("#title").textContent = manifest.title;
      document.querySelector("#meta").textContent = (manifest.authors || []).join("、");
      reader.replaceChildren();
      const images = [];
      for (const chapter of manifest.chapters) {
        const heading = document.createElement("h2");
        heading.className = "chapter-title";
        heading.id = `chapter-${chapter.id}`;
        heading.textContent = chapter.title || `章节 ${chapter.order}`;
        reader.append(heading);
        const link = document.createElement("a");
        link.href = `#${heading.id}`;
        link.textContent = heading.textContent;
        link.onclick = () => { drawer.hidden = true; };
        nav.append(link);
        for (const page of chapter.pages) {
          const box = document.createElement("article");
          box.className = "page";
          box.dataset.chapter = chapter.id;
          box.dataset.page = String(page.index);
          const img = document.createElement("img");
          img.dataset.url = page.url;
          img.alt = `${heading.textContent} 第 ${page.index} 页`;
          img.loading = "lazy";
          img.decoding = "async";
          img.addEventListener("load", () => {
            img.retryCount = 0;
            failedPages.delete(img);
            box.classList.remove("page-error");
          });
          img.addEventListener("error", () => imageFailed(img));
          box.append(img);
          reader.append(box);
          images.push(img);
        }
      }
      const loader = new IntersectionObserver((entries) => entries.forEach((entry) => {
        if (entry.isIntersecting && !entry.target.src) {
          loadImage(entry.target);
          loader.unobserve(entry.target);
        }
      }), { rootMargin: "1200px 0px" });
      images.forEach((img) => loader.observe(img));
      let saveTimer;
      const progress = new IntersectionObserver((entries) => entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        clearTimeout(saveTimer);
        saveTimer = setTimeout(() => localStorage.setItem(storageKey, JSON.stringify({
          chapter: entry.target.dataset.chapter,
          page: entry.target.dataset.page,
          updated: Date.now(),
        })), 250);
      }), { rootMargin: "-35% 0px -55%", threshold: 0 });
      document.querySelectorAll(".page").forEach((page) => progress.observe(page));
      if (saved?.chapter && saved?.page) {
        const target = document.querySelector(`[data-chapter="${CSS.escape(String(saved.chapter))}"][data-page="${CSS.escape(String(saved.page))}"]`);
        if (target) {
          resume.hidden = false;
          resume.onclick = () => {
            target.scrollIntoView({ behavior: "smooth" });
            resume.hidden = true;
          };
        }
      }
    })
    .catch(() => { reader.textContent = "漫画信息加载失败，请稍后重试。"; });
})();
