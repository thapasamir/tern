import { Button } from "@/shared/ui/button";

import { logoutAction } from "../actions";
import { getAccount } from "../account";

/** The signed-in user in the masthead: who, which workspace, and a way out. */
export async function UserMenu() {
  const account = await getAccount();
  if (!account) return null;

  return (
    <div className="flex items-center gap-4">
      <div className="hidden flex-col leading-tight sm:flex">
        <span className="font-medium text-ink">{account.user.email}</span>
        <span className="eyebrow tracking-normal normal-case">{account.tenant.name}</span>
      </div>
      <form action={logoutAction}>
        <Button type="submit" variant="outline" size="sm">
          Sign out
        </Button>
      </form>
    </div>
  );
}
