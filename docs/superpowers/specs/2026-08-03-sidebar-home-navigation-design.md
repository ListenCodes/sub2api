# Sidebar Home Navigation Design

## Goal

Clicking either the sidebar logo or site name must navigate every signed-in user to the public `/home` page.

## Scope

- Keep the existing `router-link` elements and click handling in `AppSidebar.vue`.
- Replace the role-dependent dashboard destination with the constant `/home` path.
- Update the focused sidebar contract test to assert the fixed home destination.
- Do not modify the central router, authentication behavior, release badge, or other navigation items.

## Verification

Use the existing `AppSidebar.spec.ts` suite to demonstrate the regression before the implementation change and to verify the restored behavior afterward. Also run the relevant frontend type check if available and `git diff --check`.
