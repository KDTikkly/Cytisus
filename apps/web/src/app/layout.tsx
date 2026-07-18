import type { Metadata } from "next";
import type { ReactNode } from "react";

import { enUS } from "@/i18n/en-US";

import "./globals.css";

export const metadata: Metadata = {
  title: enUS.appName,
  description: enUS.description,
};

export default function RootLayout({
  children,
}: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en-US">
      <body>{children}</body>
    </html>
  );
}
