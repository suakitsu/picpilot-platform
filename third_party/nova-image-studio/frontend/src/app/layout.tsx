import type { Metadata } from "next";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import Script from "next/script";
import "./globals.css";

export const metadata: Metadata = {
  title: "PicPilot AI 创作工作台",
  description: "PicPilot AI 图像创作工作台",
  icons: {
    icon: [
      { url: '/canvas/nova/picpilot-brand.png', type: 'image/png' },
    ],
    shortcut: '/canvas/nova/picpilot-brand.png',
    apple: '/canvas/nova/picpilot-brand.png',
  },
  other: {
    'theme-color': '#1a1a2e',
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <head>
        <Script
          id="theme-init"
          strategy="beforeInteractive"
          dangerouslySetInnerHTML={{
            __html: `
              (function() {
                try {
                  const theme = window.localStorage.getItem('theme');
                  if (theme === 'dark' || theme === 'light') {
                    document.documentElement.setAttribute('data-theme', theme);
                  } else {
                    document.documentElement.removeAttribute('data-theme');
                  }
                } catch {
                  document.documentElement.removeAttribute('data-theme');
                }
              })();
            `,
          }}
        />
        <Script
          id="wide-mode-init"
          strategy="beforeInteractive"
          dangerouslySetInnerHTML={{
            __html: `
              (function() {
                try {
                  var stored = window.localStorage.getItem('nova-wide-mode');
                  var wide = stored === 'enabled' && window.innerWidth >= 1280;
                  if (wide) {
                    document.documentElement.setAttribute('data-wide-mode', '');
                  }
                } catch {}
              })();
            `,
          }}
        />
      </head>
      <body
        className="antialiased min-h-screen bg-background text-foreground"
      >
        <div id="app-boot-loader" className="fixed inset-0 z-[99999] flex items-center justify-center bg-background" suppressHydrationWarning>
          <svg className="animate-spin h-8 w-8 text-primary" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
        <TooltipProvider>
          <ErrorBoundary>
            <main>
              {children}
            </main>
          </ErrorBoundary>
        </TooltipProvider>
      </body>
    </html>
  );
}
