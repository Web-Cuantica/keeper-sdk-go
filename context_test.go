package keeper

import (
	"context"
	"log/slog"
	"testing"
)

func TestAnnotateAcumulaEnContexto(t *testing.T) {
	ctx := ContextWithEvent(context.Background())
	Annotate(ctx, slog.Int("rcpt_id", 87772))
	Annotate(ctx, slog.String("sales_order", "1345678"))

	attrs := EventAttrs(ctx)
	if len(attrs) != 2 {
		t.Fatalf("esperaba 2 atributos, hubo %d", len(attrs))
	}
	got := map[string]slog.Value{}
	for _, a := range attrs {
		got[a.Key] = a.Value
	}
	if got["rcpt_id"].Int64() != 87772 {
		t.Errorf("rcpt_id = %v", got["rcpt_id"])
	}
	if got["sales_order"].String() != "1345678" {
		t.Errorf("sales_order = %v", got["sales_order"])
	}
}

func TestAnnotateFueraDeRequestEsNoop(t *testing.T) {
	// Sin ContextWithEvent no debe entrar en pánico ni acumular.
	ctx := context.Background()
	Annotate(ctx, slog.Int("x", 1))
	if attrs := EventAttrs(ctx); attrs != nil {
		t.Errorf("esperaba nil fuera de un request, hubo %v", attrs)
	}
}

func TestEventAttrsEsCopia(t *testing.T) {
	ctx := ContextWithEvent(context.Background())
	Annotate(ctx, slog.Int("a", 1))
	first := EventAttrs(ctx)
	Annotate(ctx, slog.Int("b", 2))
	if len(first) != 1 {
		t.Errorf("la copia previa no debe verse afectada por anotaciones nuevas: %v", first)
	}
}

func TestAnnotateUserYTenant(t *testing.T) {
	ctx := ContextWithEvent(context.Background())
	AnnotateUser(ctx, "u-4471")
	AnnotateTenant(ctx, "t-9")
	AnnotateUser(ctx, "") // no-op
	AnnotateTenant(ctx, "")

	got := map[string]string{}
	for _, a := range EventAttrs(ctx) {
		got[a.Key] = a.Value.String()
	}
	if got["enduser.id"] != "u-4471" {
		t.Errorf("enduser.id = %q", got["enduser.id"])
	}
	if got["tenant.id"] != "t-9" {
		t.Errorf("tenant.id = %q", got["tenant.id"])
	}
	if len(EventAttrs(ctx)) != 2 {
		t.Errorf("IDs vacíos no deben anotar: %v", EventAttrs(ctx))
	}
}

func TestAnnotateOutcome(t *testing.T) {
	ctx := ContextWithEvent(context.Background())
	AnnotateOutcome(ctx, false, "validation", "folio inválido")

	got := map[string]slog.Value{}
	for _, a := range EventAttrs(ctx) {
		got[a.Key] = a.Value
	}
	if got["business.success"].Bool() {
		t.Error("business.success debía ser false")
	}
	if got["error.kind"].String() != "validation" {
		t.Errorf("error.kind = %v", got["error.kind"])
	}
	if got["error.message"].String() != "folio inválido" {
		t.Errorf("error.message = %v", got["error.message"])
	}

	ctxOK := ContextWithEvent(context.Background())
	AnnotateOutcome(ctxOK, true, "", "")
	attrs := EventAttrs(ctxOK)
	if len(attrs) != 1 || attrs[0].Key != "business.success" || !attrs[0].Value.Bool() {
		t.Errorf("éxito solo debe anotar business.success=true: %v", attrs)
	}
}

func TestRedactAttrsFuncionExportada(t *testing.T) {
	setRedactKeys(defaultRedactKeys())
	out := RedactAttrs([]slog.Attr{
		slog.String("password", "x"),
		slog.String("order_id", "o-1"),
		slog.Group("meta", slog.String("token", "t")),
	})
	got := map[string]slog.Value{}
	for _, a := range out {
		got[a.Key] = a.Value
	}
	if got["password"].String() != redactCensor {
		t.Errorf("password no censurado: %v", got["password"])
	}
	if got["order_id"].String() != "o-1" {
		t.Errorf("order_id alterado: %v", got["order_id"])
	}
	for _, a := range got["meta"].Group() {
		if a.Key == "token" && a.Value.String() != redactCensor {
			t.Errorf("token anidado no censurado: %v", a.Value)
		}
	}
}
