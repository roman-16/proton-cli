import { execSync } from "child_process";
import { existsSync } from "fs";

const REPO_URL = "https://github.com/ProtonMail/WebClients.git";

export function ensureRepo(dest: string): void {
  if (existsSync(`${dest}/.git`)) {
    execSync("git pull", { cwd: dest, stdio: "pipe" });
  } else {
    execSync(`git clone --depth 1 --branch main ${REPO_URL} ${dest}`, {
      stdio: "pipe",
    });
  }
}
