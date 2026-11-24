import type { Metadata } from "next";
import type { ReactNode } from "react";
import { Toaster } from "@/components/ui/sonner";
import { themeBootScript } from "@/lib/theme";
import { Providers } from "./providers";
import "./globals.css";

export const metadata: Metadata = { title: "Agent Verdicts" };

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    // lang and the `dark` class are corrected on the client (boot script /
    // Providers), so the attributes may differ from the prerendered HTML.
    <html lang="vi" className="h-full antialiased" suppressHydrationWarning>
      <head>
        {/* Static constant (no user data): sets `dark` before first paint. */}
        <script dangerouslySetInnerHTML={{ __html: themeBootScript }} />
      </head>
      <body className="min-h-full flex flex-col">
        <Providers>
          {children}
          <Toaster />
        </Providers>
      </body>
    </html>
  );
}
