package complete

import (
	"context"
	"fmt"
	"time"

	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/util"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Revisions[
	C server.HistoryClient[T, H, HR],
	T server.ConfigType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
](ctx context.Context, req H, client C) ([]string, cobra.ShellCompDirective) {
	clone := util.ProtoClone(req)
	clone.ProtoReflect().Set(util.FieldByName[H]("includevalues"), protoreflect.ValueOfBool(false))
	history, err := client.ConfigurationHistory(ctx, clone)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	revisions := make([]string, len(history.GetEntries()))
	for i, entry := range history.GetEntries() {
		comp := fmt.Sprint(entry.GetRevision().GetRevision())
		ts := entry.GetRevision().GetTimestamp().AsTime()
		if !ts.IsZero() {
			comp = fmt.Sprintf("%s\t%s", comp, ts.Format(time.Stamp))
		}
		revisions[i] = comp
	}
	return revisions, cobra.ShellCompDirectiveNoFileComp
}
