// `import x from "./file.md?raw"`: the file's text (vitest natively, Turbopack via the rule in next.config.ts).
declare module "*.md?raw" {
  const text: string;
  export default text;
}
