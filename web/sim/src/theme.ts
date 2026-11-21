// The page follows the system's light or dark mode (it has no settings of its own).
export function followSystemTheme(): void {
  const media = window.matchMedia('(prefers-color-scheme: light)');
  const apply = () => {
    document.documentElement.dataset['theme'] = media.matches ? 'light' : 'dark';
  };
  apply();
  media.addEventListener('change', apply);
}
