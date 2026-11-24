"use client";

import { LanguagesIcon, MoonIcon, SunIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useLocale, useTheme } from "@/app/providers";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { LOCALES, type Locale } from "@/lib/i18n";

export function LocaleThemeSwitch() {
  const t = useTranslations("nav");
  const { locale, setLocale } = useLocale();
  const { dark, toggle } = useTheme();

  return (
    <div className="flex items-center gap-1">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" aria-label={t("language")}>
            <LanguagesIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup value={locale} onValueChange={(v) => setLocale(v as Locale)}>
            {LOCALES.map((l) => (
              <DropdownMenuRadioItem key={l} value={l}>
                {t(`locale_${l}`)}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Icon follows the `dark` class via CSS, so it is right before hydration too. */}
      <Button variant="ghost" size="icon" aria-label={t(dark ? "theme_to_light" : "theme_to_dark")} onClick={toggle}>
        <MoonIcon className="dark:hidden" />
        <SunIcon className="hidden dark:block" />
      </Button>
    </div>
  );
}
