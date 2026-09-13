const preview = document.getElementById('preview');
if (preview) {
  const failed = () => { preview.hidden = true; document.getElementById('preview-error').hidden = false; };
  preview.addEventListener('error', failed);
  if (preview.complete && preview.naturalWidth === 0) failed();

  const upgrade = () => {
    const source = preview.dataset.upgradeSrc;
    if (!source) return;
    delete preview.dataset.upgradeSrc;
    const clearer = new Image();
    clearer.onload = () => {
      preview.src = source;
      const link = document.getElementById('preview-link');
      if (link) link.href = source;
    };
    clearer.src = source;
  };
  preview.addEventListener('load', upgrade, { once: true });
  if (preview.complete && preview.naturalWidth > 0) upgrade();
}
