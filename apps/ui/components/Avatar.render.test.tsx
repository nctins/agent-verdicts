import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { Avatar } from "./Avatar";

it("draws an agent as a rounded square with a bot, a person as a circle with initials", () => {
  const { container } = render(<><Avatar name="Scout" type="Agent" seed="a1" /><Avatar name="Alice Lê" type="Human" seed="h1" /></>);
  const [agent, human] = [...container.querySelectorAll("[data-actor-type]")];
  expect(agent.getAttribute("data-actor-type")).toBe("agent");
  expect(agent.className).toContain("rounded-lg");
  expect(agent.querySelector("svg")).toBeTruthy();
  expect(human.className).toContain("rounded-full");
  expect(human.textContent).toBe("AL");
});
