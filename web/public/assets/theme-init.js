try {
  const theme = localStorage.getItem('family-budget-theme') === 'dark' ? 'dark' : 'light'
  document.documentElement.dataset.theme = theme
  if (theme === 'dark') document.querySelector('meta[name="theme-color"]').content = '#141c18'
} catch {
  document.documentElement.dataset.theme = 'light'
}
