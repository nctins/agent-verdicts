import { expect, it } from "vitest";
import { toCsv } from "./csv";

it("quotes every field, doubles quotes, keeps commas and newlines inside a field", () => {
  expect(toCsv([["a", 'say "hi"'], ["x,y", "line1\nline2"], [null, 3]])).toBe('﻿"a","say ""hi"""\r\n"x,y","line1\nline2"\r\n"","3"\r\n');
});
