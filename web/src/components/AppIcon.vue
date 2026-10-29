<script setup>
// 单色线性图标：统一 stroke 1.8、round 端点，颜色跟随 currentColor
// 图形基于 Feather/Lucide 的开放图形语言，按需内联，避免整包引入
const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 16 },
})

const icons = {
  mail: '<rect x="2.5" y="4.5" width="19" height="15" rx="2.5"/><path d="M3.5 6.5l8.5 6 8.5-6"/>',
  lock: '<rect x="4.5" y="10.5" width="15" height="10" rx="2"/><path d="M8 10.5V7a4 4 0 0 1 8 0v3.5"/>',
  unlock: '<rect x="4.5" y="10.5" width="15" height="10" rx="2"/><path d="M8 10.5V7a4 4 0 0 1 7.7-1.5"/>',
  key: '<path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.8 7.8 5.5 5.5 0 0 1 7.8-7.8zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>',
  trash: '<path d="M3.5 6.5h17"/><path d="M18.5 6.5v13a2 2 0 0 1-2 2h-9a2 2 0 0 1-2-2v-13"/><path d="M9.5 6.5v-2a2 2 0 0 1 2-2h1a2 2 0 0 1 2 2v2"/><path d="M10 11v6M14 11v6"/>',
  save: '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/>',
  eraser: '<path d="M7 21h11"/><path d="M5.5 13.5L13 6a2.1 2.1 0 0 1 3 0l2 2a2.1 2.1 0 0 1 0 3l-6.5 6.5H7l-1.5-1.5a2.1 2.1 0 0 1 0-2.5z"/>',
  alert: '<path d="M10.3 3.9L1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4.5"/><path d="M12 17h.01"/>',
  menu: '<path d="M3.5 6.5h17"/><path d="M3.5 12h17"/><path d="M3.5 17.5h17"/>',
  pin: '<path d="M12 16.5V22"/><path d="M9 10.8a2 2 0 0 1-1.1 1.8l-1.8.9A2 2 0 0 0 5 15.2V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.8a2 2 0 0 0-1.1-1.8l-1.8-.9a2 2 0 0 1-1.1-1.8V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
  sun: '<circle cx="12" cy="12" r="4.5"/><path d="M12 2v2.5M12 19.5V22M2 12h2.5M19.5 12H22M4.9 4.9l1.8 1.8M17.3 17.3l1.8 1.8M2.5 21.5M4.9 19.1l1.8-1.8M17.3 6.7l1.8-1.8"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  contrast: '<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none"/>',
  file: '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/>',
  'file-text': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><path d="M8.5 13.5h7M8.5 17h4.5"/>',
  'file-code': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><path d="M10.5 12.5L8.5 14.5l2 2"/><path d="M13.5 12.5l2 2-2 2"/>',
  'file-archive': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><path d="M10 6h.01M10 9h.01"/><rect x="8.8" y="12" width="2.4" height="4.5" rx="1.2"/>',
  'file-sheet': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><path d="M8.5 12.5h7v5.5h-7z"/><path d="M12 12.5v5.5"/>',
  'file-doc': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><path d="M8.5 13h7M8.5 16.5h7"/>',
  'file-slide': '<path d="M14 2.5H6a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8.5z"/><path d="M14 2.5v6h6"/><rect x="8.5" y="12.5" width="7" height="5" rx="0.8"/>',
}
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    v-html="icons[name] || icons.file"
  ></svg>
</template>
