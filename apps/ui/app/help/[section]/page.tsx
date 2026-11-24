import { HelpMovedRedirect, HelpSectionView, HelpShell } from "@/components/Help";
import { HELP_MOVED, HELP_SECTIONS, type HelpSection } from "@/lib/sections";

// One static page per help section (static export: the list is fixed at build time), plus the old ids that moved.
export const dynamicParams = false;
export function generateStaticParams() {
  return [...HELP_SECTIONS, ...Object.keys(HELP_MOVED)].map((section) => ({ section }));
}

export default async function HelpSectionPage({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params;
  const moved = HELP_MOVED[section];
  return (
    <HelpShell>
      {moved ? <HelpMovedRedirect to={moved as HelpSection} /> : <HelpSectionView id={section as HelpSection} />}
    </HelpShell>
  );
}
