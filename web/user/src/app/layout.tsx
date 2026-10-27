import type { Metadata } from 'next'
import './globals.css'
import './edition.css'
import { AppRuntime } from '@/components/AppRuntime'
import { ConfirmProvider } from '@/components/ConfirmProvider'

export const metadata: Metadata = {
  title: 'PicPilot',
  description: 'PicPilot AI product image generation platform',
  icons: {
    icon: '/favicon.png',
    shortcut: '/favicon.png',
    apple: '/brand-picpilot-mark.png'
  }
}

const themeScript = `
(function(){
  try {
    var mode = localStorage.getItem('openimage-theme') || 'system';
    if (!['light','dark','system'].includes(mode)) mode = 'system';
    var resolved = mode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : mode === 'dark' ? 'dark' : 'light';
    document.documentElement.dataset.themeMode = mode;
    document.documentElement.dataset.theme = resolved;
    document.documentElement.style.colorScheme = resolved;
    var motion = localStorage.getItem('openimage-motion') || 'on';
    document.documentElement.dataset.motion = motion === 'off' ? 'off' : 'on';
    var lang = localStorage.getItem('openimage-language') || 'zh-CN';
    if (!['zh-CN','en-US','ja-JP'].includes(lang)) lang = 'zh-CN';
    document.documentElement.lang = lang;
    var client = new URLSearchParams(window.location.search).get('client');
    var userAgent = navigator.userAgent || '';
    if (userAgent.indexOf('PicPilotAndroid/') !== -1) client = 'android';
    if (userAgent.indexOf('PicPilotWindows/') !== -1) client = 'windows';
    if (client === 'android' || client === 'windows') document.documentElement.dataset.picpilotClient = client;
    var accent = localStorage.getItem('openimage-accent') || '#2563eb';
    var match = accent.replace('#','').match(/^[0-9a-fA-F]{6}$/);
    if (match) {
      var r = parseInt(accent.slice(1,3), 16);
      var g = parseInt(accent.slice(3,5), 16);
      var b = parseInt(accent.slice(5,7), 16);
      document.documentElement.style.setProperty('--accent-rgb', r + ',' + g + ',' + b);
      document.documentElement.style.setProperty('--primary', 'rgb(' + r + ',' + g + ',' + b + ')');
      document.documentElement.style.setProperty('--primary-hover', 'rgb(' + Math.max(0,r-20) + ',' + Math.max(0,g-20) + ',' + Math.max(0,b-20) + ')');
      document.documentElement.style.setProperty('--primary-soft', 'rgba(' + r + ',' + g + ',' + b + ',.14)');
    }
  } catch (e) {}
})();`

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return <html lang="zh-CN" suppressHydrationWarning><head><script dangerouslySetInnerHTML={{ __html: themeScript }} /></head><body><ConfirmProvider><AppRuntime>{children}</AppRuntime></ConfirmProvider></body></html>
}
