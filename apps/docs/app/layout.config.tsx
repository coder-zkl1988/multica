import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { MulticaWordmark } from "@multica/ui/components/common/multica-wordmark";

// External links are intentionally absent from the docs sidebar (fork commit
// be96ddd14c); the brand wordmark from upstream's brand v3.1 pass stays.
export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="inline-flex items-center gap-2 font-semibold text-base">
        <MulticaWordmark className="h-[15px]" />
        Docs
      </span>
    ),
  },
};
