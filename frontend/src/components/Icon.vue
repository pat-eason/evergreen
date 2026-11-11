<script setup lang="ts">
const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
const paths: Record<string, string> = {
  home: 'M3 11l9-8 9 8M5 10v10h5v-6h4v6h5V10',
  wallet: 'M3 7a2 2 0 012-2h13v4M3 7v11a2 2 0 002 2h15a1 1 0 001-1v-9a1 1 0 00-1-1H5a2 2 0 01-2-2zM16.5 14.5h.01',
  calendar: 'M7 3v4M17 3v4M4 9h16M5 5h14a1 1 0 011 1v13a1 1 0 01-1 1H5a1 1 0 01-1-1V6a1 1 0 011-1z',
  chart: 'M4 20V10M10 20V4M16 20v-7M22 20H2',
  layers: 'M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5',
  spark: 'M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3zM19 17l.8 2.2L22 20l-2.2.8L19 23l-.8-2.2L16 20l2.2-.8L19 17z',
  plus: 'M12 5v14M5 12h14',
  edit: 'M4 20h4l10.5-10.5a2.1 2.1 0 00-3-3L5 17v3zM13.5 7.5l3 3',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 12a1 1 0 001 1h8a1 1 0 001-1l1-12M9 7V4h6v3',
  close: 'M6 6l12 12M18 6L6 18',
  logout: 'M15 4h3a2 2 0 012 2v12a2 2 0 01-2 2h-3M10 16l-4-4 4-4M6 12h10',
  chevron: 'M6 9l6 6 6-6',
  left: 'M15 6l-6 6 6 6',
  right: 'M9 6l6 6-6 6',
  bank: 'M3 10l9-6 9 6M5 10v8M9 10v8M15 10v8M19 10v8M3 20h18',
  trend: 'M3 17l6-6 4 4 8-9M15 6h6v6',
  card: 'M3 7a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2V7zM3 10h18M7 15h3',
  house: 'M4 20V10l8-6 8 6v10H4zM10 20v-6h4v6',
  debt: 'M12 3v18M16.5 7.5C15.7 6.6 14.1 6 12.5 6 10.3 6 8.5 7.1 8.5 8.8c0 3.7 7.5 1.9 7.5 5.6 0 1.8-1.9 3-4 3-1.8 0-3.4-.7-4.3-1.8',
  save: 'M5 4h11l4 4v11a1 1 0 01-1 1H5a1 1 0 01-1-1V5a1 1 0 011-1zM8 4v5h7V4M8 20v-6h8v6',
  check: 'M5 12.5l4.5 4.5L19 7.5',
  copy: 'M9 9h10v11H9zM5 15V4h10',
  sliders: 'M4 6h10M18 6h2M4 12h2M10 12h10M4 18h12M20 18h0M14 4v4M8 10v4M18 16v4',
  info: 'M12 8h.01M11 12h1v5h1M12 21a9 9 0 100-18 9 9 0 000 18z',
  target: 'M12 21a9 9 0 100-18 9 9 0 000 18zM12 16a4 4 0 100-8 4 4 0 000 8zM12 12h.01',
  history: 'M3 12a9 9 0 109-9 9 9 0 00-6.4 2.6L3 8M3 3v5h5M12 7v5l3 2',
  umbrella: 'M12 3a9 9 0 019 9H3a9 9 0 019-9zM12 12v6a2 2 0 004 0',
  heart: 'M12 20s-7-4.4-7-10a4 4 0 017-2.6A4 4 0 0119 10c0 5.600-7 10-7 10z',
  coffee: 'M4 9h12v5a5 5 0 01-5 5H9a5 5 0 01-5-5V9zM16 10h2a2 2 0 010 4h-2M7 3v3M11 3v3',
  piggy: 'M5 11a7 7 0 0112-3h2v3l-1 1a6 6 0 01-2 4v2h-3v-1H9v1H6v-3a6 6 0 01-1-4zM15 11h.01',
  palm: 'M12 21V10M12 10c-1-4-5-5-8-3 3 0 5 1 6 3M12 10c1-4 5-5 8-3-3 0-5 1-6 3M12 10c-3-1-5 1-6 4 2-2 4-2 6-1M12 10c3-1 5 1 6 4-2-2-4-2-6-1',
  eye: 'M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12zM12 15a3 3 0 100-6 3 3 0 000 6z',
  'eye-off': 'M3 3l18 18M10.6 5.1A9.9 9.9 0 0112 5c6.4 0 10 7 10 7a17 17 0 01-3.2 4.1M6.6 6.6A16.6 16.6 0 002 12s3.6 7 10 7a9.700 9.700 0 004.4-1.100M9.9 9.9a3 3 0 004.2 4.2',
  shield: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6l8-3z',
}
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path :d="paths[props.name] ?? paths.info" />
  </svg>
</template>
