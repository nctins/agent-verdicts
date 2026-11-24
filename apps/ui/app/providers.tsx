"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NextIntlClientProvider } from "next-intl";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { LOCALE_KEY, resolveLocale, type Locale } from "@/lib/i18n";
import { DARK_QUERY, nextTheme, readThemePref, resolveTheme, THEME_KEY, type ThemePref } from "@/lib/theme";
import { readStorage, writeStorage } from "@/lib/utils";
import en from "@/messages/en.json";
import vi from "@/messages/vi.json";

const messages = { vi, en };

type LocaleCtx = { locale: Locale; setLocale: (l: Locale) => void };
type ThemeCtx = { pref: ThemePref; dark: boolean; toggle: () => void };

const LocaleContext = createContext<LocaleCtx | null>(null);
const ThemeContext = createContext<ThemeCtx | null>(null);

export function useLocale(): LocaleCtx {
  const c = useContext(LocaleContext);
  if (!c) throw new Error("useLocale outside <Providers>");
  return c;
}

export function useTheme(): ThemeCtx {
  const c = useContext(ThemeContext);
  if (!c) throw new Error("useTheme outside <Providers>");
  return c;
}

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { refetchOnWindowFocus: true, staleTime: 10_000, retry: 3 } },
      }),
  );
  // The static HTML is prerendered with the defaults; the real choice is read
  // after hydration so server and client markup match.
  const [locale, setLocaleState] = useState<Locale>("vi");
  const [pref, setPrefState] = useState<ThemePref>("system");
  const [timeZone, setTimeZone] = useState("UTC");
  const [dark, setDark] = useState(false); // what is on screen (pref resolved against the OS)
  // The default "now" of relative times. Taken in the browser (state is not part of the static HTML) and moved
  // forward whenever data arrives, so a post that was just fetched or created is never dated in the future.
  const [now, setNow] = useState(() => new Date());
  useEffect(
    () => queryClient.getQueryCache().subscribe((e) => e.type === "updated" && e.action.type === "success" && setNow(new Date())),
    [queryClient],
  );

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-time read of browser-only state after hydration
    setLocaleState(resolveLocale(readStorage(LOCALE_KEY), navigator.language));
    setPrefState(readThemePref(readStorage(THEME_KEY)));
    setTimeZone(Intl.DateTimeFormat().resolvedOptions().timeZone);
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  useEffect(() => {
    const mq = window.matchMedia(DARK_QUERY);
    const apply = () => {
      const d = resolveTheme(pref, mq.matches) === "dark";
      document.documentElement.classList.toggle("dark", d);
      setDark(d);
    };
    apply();
    if (pref !== "system") return;
    mq.addEventListener("change", apply);
    return () => mq.removeEventListener("change", apply);
  }, [pref]);

  const setLocale = (l: Locale) => {
    writeStorage(LOCALE_KEY, l);
    setLocaleState(l);
  };
  const toggle = () => {
    const p = nextTheme(pref, window.matchMedia(DARK_QUERY).matches);
    writeStorage(THEME_KEY, p);
    setPrefState(p);
  };

  return (
    <QueryClientProvider client={queryClient}>
      <LocaleContext.Provider value={{ locale, setLocale }}>
        <ThemeContext.Provider value={{ pref, dark, toggle }}>
          <NextIntlClientProvider locale={locale} messages={messages[locale]} timeZone={timeZone} now={now}>
            {children}
          </NextIntlClientProvider>
        </ThemeContext.Provider>
      </LocaleContext.Provider>
    </QueryClientProvider>
  );
}
