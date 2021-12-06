import { css } from '@emotion/css';
import { GrafanaTheme2, LoadingState } from '@grafana/data';
import { Button, Switch, useStyles2 } from '@grafana/ui';
import Stack from 'app/plugins/datasource/cloudwatch/components/ui/Stack';
import React, { useCallback } from 'react';
import EditorHeader from '../../../cloudwatch/components/ui/EditorHeader';
import FlexItem from '../../../cloudwatch/components/ui/FlexItem';
import InlineSelect from '../../../cloudwatch/components/ui/InlineSelect';
import { Space } from '../../../cloudwatch/components/ui/Space';
import { PromQueryEditor } from '../../components/PromQueryEditor';
import { PromQueryEditorProps } from '../../components/types';
import { QueryEditorModeToggle } from '../shared/QueryEditorModeToggle';
import { QueryEditorMode } from '../shared/types';
import { PromQueryBuilder } from './PromQueryBuilder';

export const PromQueryEditorSelector = React.memo<PromQueryEditorProps>((props) => {
  const { query, onChange, onRunQuery, data } = props;
  const styles = useStyles2(getStyles);

  const onEditorModeChange = useCallback(
    (newMetricEditorMode: QueryEditorMode) => {
      onChange({ ...query, editorMode: newMetricEditorMode });
    },
    [onChange, query]
  );

  // If no expr (ie new query) then default to builder
  const editorMode = query.editorMode ?? (query.expr ? QueryEditorMode.Code : QueryEditorMode.Builder);

  return (
    <>
      <EditorHeader>
        <FlexItem grow={1} />
        <Button
          className={styles.runQuery}
          variant="secondary"
          size="sm"
          fill="outline"
          onClick={onRunQuery}
          disabled={data?.state === LoadingState.Loading}
        >
          Run query
        </Button>
        <Stack gap={1}>
          <label className={styles.switchLabel}>Instant</label>
          <Switch />
        </Stack>
        <Stack gap={1}>
          <label className={styles.switchLabel}>Exemplars</label>
          <Switch />
        </Stack>
        <InlineSelect
          value={undefined}
          placeholder="Query patterns"
          allowCustomValue
          onChange={({ value }) => {}}
          options={[]}
        />
        <QueryEditorModeToggle mode={editorMode} onChange={onEditorModeChange} />
      </EditorHeader>
      <Space v={0.5} />
      {editorMode === QueryEditorMode.Code && <PromQueryEditor {...props} />}
      {editorMode === QueryEditorMode.Builder && <PromQueryBuilder {...props} />}
    </>
  );
});

PromQueryEditorSelector.displayName = 'PromQueryEditorSelector';

const getStyles = (theme: GrafanaTheme2) => {
  return {
    runQuery: css({
      color: theme.colors.text.secondary,
    }),
    switchLabel: css({
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
    }),
  };
};
