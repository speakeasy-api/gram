import { useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  errorMessage,
  gramAdminFetch,
  gramAdminMutation,
} from "@/lib/gramAdminApi";

type SignupOffer = {
  id: string;
  name: string;
  stripe_promotion_code_id: string;
  signup_flow: "tailscale_partner_gateway";
  enabled: boolean;
  created_at: string;
  updated_at: string;
};
type OfferInput = Pick<
  SignupOffer,
  "name" | "stripe_promotion_code_id" | "signup_flow" | "enabled"
> & { id?: string };
const offersKey = ["admin", "signup-offers"];

function saveOffer(offer: OfferInput) {
  return gramAdminMutation<SignupOffer>(
    `/admin/signupOffers.${offer.id ? "update" : "create"}`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: offer.name,
        stripe_promotion_code_id: offer.stripe_promotion_code_id,
        signup_flow: offer.signup_flow,
        ...(offer.id ? { id: offer.id, enabled: offer.enabled } : {}),
      }),
    },
  );
}

export function SignupOffersPage(): JSX.Element {
  const client = useQueryClient();
  const offers = useQuery({
    queryKey: offersKey,
    queryFn: () =>
      gramAdminFetch<{ offers: SignupOffer[] }>("/admin/signupOffers"),
  });
  const [editing, setEditing] = useState<OfferInput | null>(null);
  const mutation = useMutation({
    mutationFn: saveOffer,
    onSuccess: async () => {
      setEditing(null);
      await client.invalidateQueries({ queryKey: offersKey });
    },
  });
  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Signup offers</h1>
          <p className="text-muted-foreground">
            Map exact Stripe promotion code IDs to signup flows. Stripe owns all
            discount terms.
          </p>
        </div>
        <Button
          disabled={mutation.isPending}
          onClick={() => {
            mutation.reset();
            setEditing({
              name: "",
              stripe_promotion_code_id: "",
              signup_flow: "tailscale_partner_gateway",
              enabled: false,
            });
          }}
        >
          Create offer
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        New offers are disabled. Disabling or editing an offer does not change
        already accepted promotion applications.
      </p>
      {offers.isPending && <p>Loading offers…</p>}
      {offers.isError && (
        <div role="alert">
          {errorMessage(offers.error)}{" "}
          <Button onClick={() => void offers.refetch()}>Retry</Button>
        </div>
      )}
      {mutation.isError && !editing && (
        <p role="alert">{errorMessage(mutation.error)}</p>
      )}
      {offers.data && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Stripe promotion code ID</TableHead>
              <TableHead>Signup flow</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {offers.data.offers.map((offer) => (
              <TableRow key={offer.id}>
                <TableCell>{offer.name}</TableCell>
                <TableCell className="font-mono">
                  {offer.stripe_promotion_code_id}
                </TableCell>
                <TableCell>Tailscale Partner Gateway</TableCell>
                <TableCell>{offer.enabled ? "Enabled" : "Disabled"}</TableCell>
                <TableCell className="space-x-2">
                  <Button
                    variant="outline"
                    disabled={mutation.isPending}
                    aria-label={`Edit ${offer.name}`}
                    onClick={() => {
                      mutation.reset();
                      setEditing(offer);
                    }}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="outline"
                    disabled={mutation.isPending}
                    aria-label={`${offer.enabled ? "Disable" : "Enable"} ${offer.name}`}
                    onClick={() =>
                      mutation.mutate({ ...offer, enabled: !offer.enabled })
                    }
                  >
                    {offer.enabled ? "Disable" : "Enable"}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {offers.data?.offers.length === 0 && (
        <p>
          No signup offers. Create an offer to configure a supported signup
          promotion.
        </p>
      )}
      <Dialog
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setEditing(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing?.id ? "Edit signup offer" : "Create signup offer"}
            </DialogTitle>
            <DialogDescription>
              Use the exact Stripe promotion code ID, not the customer-facing
              code. Configure discount terms in Stripe.
            </DialogDescription>
          </DialogHeader>
          {editing && (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault();
                mutation.mutate(editing);
              }}
            >
              <label className="block space-y-1">
                Name
                <Input
                  required
                  maxLength={200}
                  value={editing.name}
                  onChange={(event) =>
                    setEditing({ ...editing, name: event.target.value })
                  }
                />
              </label>
              <label className="block space-y-1">
                Stripe promotion code ID
                <Input
                  required
                  maxLength={255}
                  pattern="promo_[A-Za-z0-9_]+"
                  placeholder="promo_…"
                  value={editing.stripe_promotion_code_id}
                  onChange={(event) =>
                    setEditing({
                      ...editing,
                      stripe_promotion_code_id: event.target.value,
                    })
                  }
                />
              </label>
              <div>
                <span className="text-sm font-medium">Signup flow</span>
                <p>Tailscale Partner Gateway</p>
              </div>
              {mutation.isError && (
                <p role="alert">{errorMessage(mutation.error)}</p>
              )}
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={mutation.isPending}
                  onClick={() => setEditing(null)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={mutation.isPending || !editing.name.trim()}
                >
                  {mutation.isPending ? "Saving…" : "Save offer"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
