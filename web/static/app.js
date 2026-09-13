const preview = document.getElementById('preview');
if (preview) {
  const failed = () => { preview.hidden = true; document.getElementById('preview-error').hidden = false; };
  preview.addEventListener('error', failed);
  if (preview.complete && preview.naturalWidth === 0) failed();
}
